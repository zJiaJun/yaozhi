// SPDX-License-Identifier: GPL-3.0-only

package corebluetooth

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego/objc"
)

// State mirrors CBManagerState.
type State int

const (
	StateUnknown State = iota
	StateResetting
	StateUnsupported
	StateUnauthorized
	StatePoweredOff
	StatePoweredOn
)

func (s State) String() string {
	switch s {
	case StateUnknown:
		return "unknown"
	case StateResetting:
		return "resetting"
	case StateUnsupported:
		return "unsupported"
	case StateUnauthorized:
		return "unauthorized"
	case StatePoweredOff:
		return "powered_off"
	case StatePoweredOn:
		return "powered_on"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Property mirrors CBCharacteristicProperties.
type Property uint

const (
	PropRead                 Property = 0x02
	PropWriteWithoutResponse Property = 0x04
	PropWrite                Property = 0x08
	PropNotify               Property = 0x10
	PropIndicate             Property = 0x20
)

// Advertisement holds the fields of advertisement data this package reads.
type Advertisement struct {
	LocalName    string
	ServiceUUIDs []string
}

// Delegate receives CoreBluetooth callbacks. Every function runs on the
// Central's serial queue, one at a time; nil functions are skipped. The
// Peripheral, Service and Characteristic values passed in may be used, and
// their methods called, only from Delegate functions or from functions passed
// to Central.Do. Do not block in them.
type Delegate struct {
	StateChanged              func(State)
	Discovered                func(p Peripheral, adv Advertisement, rssi int)
	Connected                 func(p Peripheral)
	FailedToConnect           func(p Peripheral, err error)
	Disconnected              func(p Peripheral, err error)
	ServicesDiscovered        func(p Peripheral, err error)
	CharacteristicsDiscovered func(p Peripheral, s Service, err error)
	NotifyStateUpdated        func(p Peripheral, c Characteristic, err error)
	ValueUpdated              func(p Peripheral, c Characteristic, value []byte, err error)
	ValueWritten              func(p Peripheral, c Characteristic, err error)
}

// Central owns one CBCentralManager and the serial queue its callbacks run on.
type Central struct {
	d        *Delegate
	queue    uintptr
	manager  objc.ID
	delegate objc.ID
	retained map[objc.ID]bool // peripherals kept alive by Connect; queue only
}

var (
	centrals sync.Map // delegate objc.ID -> *Central

	selState               = objc.RegisterName("state")
	selRetrieveConnected   = objc.RegisterName("retrieveConnectedPeripheralsWithServices:")
	selRetrieveIdentifiers = objc.RegisterName("retrievePeripheralsWithIdentifiers:")
	selScan                = objc.RegisterName("scanForPeripheralsWithServices:options:")
	selStopScan            = objc.RegisterName("stopScan")
	selConnect             = objc.RegisterName("connectPeripheral:options:")
	selCancelConnect       = objc.RegisterName("cancelPeripheralConnection:")
	selSetDelegate         = objc.RegisterName("setDelegate:")
)

// NewCentral creates a CBCentralManager whose callbacks go to d. The first
// StateChanged callback arrives asynchronously; wait for StatePoweredOn
// before scanning or connecting. Creating the manager is what makes macOS
// ask for Bluetooth permission.
func NewCentral(d *Delegate) (*Central, error) {
	if err := load(); err != nil {
		return nil, err
	}
	if d == nil {
		d = &Delegate{}
	}
	c := &Central{
		d:        d,
		queue:    newSerialQueue("yaozhi.corebluetooth"),
		retained: map[objc.ID]bool{},
	}
	withPool(func() {
		c.delegate = objc.ID(clsDelegate).Send(selNew)
		centrals.Store(c.delegate, c)
		c.manager = objc.ID(clsCBCentralManager).Send(selAlloc).
			Send(selInitWithDelegateQueue, c.delegate, c.queue, objc.ID(0))
	})
	if c.manager == 0 {
		centrals.Delete(c.delegate)
		c.delegate.Send(selRelease)
		return nil, fmt.Errorf("corebluetooth: CBCentralManager init failed")
	}
	return c, nil
}

// Do runs f on the Central's queue. Use it to call Central or Peripheral
// methods from other goroutines.
func (c *Central) Do(f func()) { dispatchAsync(c.queue, f) }

// State returns the manager's current state. Queue only.
func (c *Central) State() State { return State(sendInt(c.manager, selState)) }

// RetrieveConnected lists peripherals that are connected to this Mac (by any
// app, including the system's HID driver) and expose all of the given
// services. A paired remote usually stays connected and stops advertising,
// so this is how it is found. Queue only.
func (c *Central) RetrieveConnected(serviceUUIDs ...string) []Peripheral {
	return c.peripherals(c.manager.Send(selRetrieveConnected, cbUUIDs(serviceUUIDs)))
}

// RetrieveByIdentifier looks up peripherals this Mac has seen before by their
// CoreBluetooth identifier. Queue only.
func (c *Central) RetrieveByIdentifier(ids ...string) []Peripheral {
	return c.peripherals(c.manager.Send(selRetrieveIdentifiers, nsUUIDs(ids)))
}

// Scan starts scanning; no UUIDs means every advertising peripheral. Queue
// only.
func (c *Central) Scan(serviceUUIDs ...string) {
	c.manager.Send(selScan, cbUUIDs(serviceUUIDs), objc.ID(0))
}

// StopScan stops scanning. Queue only.
func (c *Central) StopScan() { c.manager.Send(selStopScan) }

// Connect connects p and keeps it alive until Close; CoreBluetooth cancels a
// pending connection when the last reference to the peripheral goes away.
// Queue only.
func (c *Central) Connect(p Peripheral) {
	if !c.retained[p.id] {
		p.id.Send(selRetain)
		c.retained[p.id] = true
	}
	p.id.Send(selSetDelegate, c.delegate)
	c.manager.Send(selConnect, p.id, objc.ID(0))
}

// CancelConnect disconnects p (this app's link only; the system may keep its
// own). Queue only.
func (c *Central) CancelConnect(p Peripheral) { c.manager.Send(selCancelConnect, p.id) }

// Close disconnects every peripheral passed to Connect and releases them.
// Queue only; the Central must not be used afterwards.
func (c *Central) Close() {
	c.manager.Send(selStopScan)
	for id := range c.retained {
		c.manager.Send(selCancelConnect, id)
		id.Send(selSetDelegate, objc.ID(0))
		id.Send(selRelease)
	}
	clear(c.retained)
}

func (c *Central) peripherals(arr objc.ID) []Peripheral {
	items := arrayItems(arr)
	ps := make([]Peripheral, len(items))
	for i, id := range items {
		ps[i] = Peripheral{id: id}
	}
	return ps
}

// Peripheral wraps a CBPeripheral. Queue only.
type Peripheral struct{ id objc.ID }

var (
	selName                    = objc.RegisterName("name")
	selIdentifier              = objc.RegisterName("identifier")
	selDiscoverServices        = objc.RegisterName("discoverServices:")
	selServices                = objc.RegisterName("services")
	selDiscoverCharacteristics = objc.RegisterName("discoverCharacteristics:forService:")
	selSetNotify               = objc.RegisterName("setNotifyValue:forCharacteristic:")
	selWriteValue              = objc.RegisterName("writeValue:forCharacteristic:type:")
	selReadValue               = objc.RegisterName("readValueForCharacteristic:")
	selUUID                    = objc.RegisterName("UUID")
	selCharacteristics         = objc.RegisterName("characteristics")
	selProperties              = objc.RegisterName("properties")
	selIsNotifying             = objc.RegisterName("isNotifying")
)

// Name returns the peripheral's GAP name, or "" if unknown.
func (p Peripheral) Name() string { return goString(p.id.Send(selName)) }

// Identifier returns the CoreBluetooth identifier, stable for this Mac.
func (p Peripheral) Identifier() string { return uuidString(p.id.Send(selIdentifier)) }

// Equal reports whether p and q are the same CBPeripheral object.
func (p Peripheral) Equal(q Peripheral) bool { return p.id == q.id }

// DiscoverServices starts service discovery; no UUIDs means all services.
func (p Peripheral) DiscoverServices(uuids ...string) {
	p.id.Send(selDiscoverServices, cbUUIDs(uuids))
}

// Services lists the services discovered so far.
func (p Peripheral) Services() []Service {
	items := arrayItems(p.id.Send(selServices))
	s := make([]Service, len(items))
	for i, id := range items {
		s[i] = Service{id: id}
	}
	return s
}

// DiscoverCharacteristics starts characteristic discovery for s.
func (p Peripheral) DiscoverCharacteristics(s Service, uuids ...string) {
	p.id.Send(selDiscoverCharacteristics, cbUUIDs(uuids), s.id)
}

// SetNotify subscribes to (or unsubscribes from) c.
func (p Peripheral) SetNotify(c Characteristic, enabled bool) {
	p.id.Send(selSetNotify, enabled, c.id)
}

// Write writes value to c. withResponse selects CBCharacteristicWriteWithResponse.
func (p Peripheral) Write(c Characteristic, value []byte, withResponse bool) {
	typ := 1 // CBCharacteristicWriteWithoutResponse
	if withResponse {
		typ = 0
	}
	p.id.Send(selWriteValue, nsData(value), c.id, typ)
}

// Read requests c's value; it arrives through Delegate.ValueUpdated.
func (p Peripheral) Read(c Characteristic) { p.id.Send(selReadValue, c.id) }

// Service wraps a CBService. Queue only.
type Service struct{ id objc.ID }

// UUID returns the service UUID as CoreBluetooth formats it (16-bit UUIDs
// in short form, e.g. "180A").
func (s Service) UUID() string { return uuidString(s.id.Send(selUUID)) }

// Characteristics lists the characteristics discovered so far.
func (s Service) Characteristics() []Characteristic {
	items := arrayItems(s.id.Send(selCharacteristics))
	cs := make([]Characteristic, len(items))
	for i, id := range items {
		cs[i] = Characteristic{id: id}
	}
	return cs
}

// Characteristic wraps a CBCharacteristic. Queue only.
type Characteristic struct{ id objc.ID }

// UUID returns the characteristic UUID as CoreBluetooth formats it.
func (c Characteristic) UUID() string { return uuidString(c.id.Send(selUUID)) }

// Properties returns the characteristic's properties.
func (c Characteristic) Properties() Property { return Property(sendUint(c.id, selProperties)) }

// IsNotifying reports whether notifications are enabled.
func (c Characteristic) IsNotifying() bool { return sendBool(c.id, selIsNotifying) }

// --- delegate class ---------------------------------------------------------

var (
	clsDelegate objc.Class

	advLocalNameKey    = "kCBAdvDataLocalName"
	advServiceUUIDsKey = "kCBAdvDataServiceUUIDs"
)

func centralFor(self objc.ID) *Central {
	c, ok := centrals.Load(self)
	if !ok {
		return nil
	}
	return c.(*Central)
}

// registerDelegateClass defines YZBluetoothDelegate, one object of which acts
// as both CBCentralManagerDelegate and CBPeripheralDelegate for a Central.
// purego callbacks are never freed, so the class is registered once.
func registerDelegateClass() error {
	var protocols []*objc.Protocol
	for _, name := range []string{"CBCentralManagerDelegate", "CBPeripheralDelegate"} {
		if p := objc.GetProtocol(name); p != nil {
			protocols = append(protocols, p)
		}
	}
	type id = objc.ID
	type sel = objc.SEL
	methods := []objc.MethodDef{
		{Cmd: objc.RegisterName("centralManagerDidUpdateState:"), Fn: func(self id, _ sel, m id) {
			if c := centralFor(self); c != nil && c.d.StateChanged != nil {
				c.d.StateChanged(State(sendInt(m, selState)))
			}
		}},
		{Cmd: objc.RegisterName("centralManager:didDiscoverPeripheral:advertisementData:RSSI:"), Fn: func(self id, _ sel, _ id, p id, adv id, rssi id) {
			if c := centralFor(self); c != nil && c.d.Discovered != nil {
				c.d.Discovered(Peripheral{p}, parseAdvertisement(adv), int(int32(sendInt(rssi, selIntValue))))
			}
		}},
		{Cmd: objc.RegisterName("centralManager:didConnectPeripheral:"), Fn: func(self id, _ sel, _ id, p id) {
			if c := centralFor(self); c != nil && c.d.Connected != nil {
				c.d.Connected(Peripheral{p})
			}
		}},
		{Cmd: objc.RegisterName("centralManager:didFailToConnectPeripheral:error:"), Fn: func(self id, _ sel, _ id, p id, e id) {
			if c := centralFor(self); c != nil && c.d.FailedToConnect != nil {
				c.d.FailedToConnect(Peripheral{p}, goError(e))
			}
		}},
		{Cmd: objc.RegisterName("centralManager:didDisconnectPeripheral:error:"), Fn: func(self id, _ sel, _ id, p id, e id) {
			if c := centralFor(self); c != nil && c.d.Disconnected != nil {
				c.d.Disconnected(Peripheral{p}, goError(e))
			}
		}},
		{Cmd: objc.RegisterName("peripheral:didDiscoverServices:"), Fn: func(self id, _ sel, p id, e id) {
			if c := centralFor(self); c != nil && c.d.ServicesDiscovered != nil {
				c.d.ServicesDiscovered(Peripheral{p}, goError(e))
			}
		}},
		{Cmd: objc.RegisterName("peripheral:didDiscoverCharacteristicsForService:error:"), Fn: func(self id, _ sel, p id, s id, e id) {
			if c := centralFor(self); c != nil && c.d.CharacteristicsDiscovered != nil {
				c.d.CharacteristicsDiscovered(Peripheral{p}, Service{s}, goError(e))
			}
		}},
		{Cmd: objc.RegisterName("peripheral:didUpdateNotificationStateForCharacteristic:error:"), Fn: func(self id, _ sel, p id, ch id, e id) {
			if c := centralFor(self); c != nil && c.d.NotifyStateUpdated != nil {
				c.d.NotifyStateUpdated(Peripheral{p}, Characteristic{ch}, goError(e))
			}
		}},
		{Cmd: objc.RegisterName("peripheral:didUpdateValueForCharacteristic:error:"), Fn: func(self id, _ sel, p id, ch id, e id) {
			if c := centralFor(self); c != nil && c.d.ValueUpdated != nil {
				c.d.ValueUpdated(Peripheral{p}, Characteristic{ch}, goBytes(ch.Send(selValue)), goError(e))
			}
		}},
		{Cmd: objc.RegisterName("peripheral:didWriteValueForCharacteristic:error:"), Fn: func(self id, _ sel, p id, ch id, e id) {
			if c := centralFor(self); c != nil && c.d.ValueWritten != nil {
				c.d.ValueWritten(Peripheral{p}, Characteristic{ch}, goError(e))
			}
		}},
	}
	cls, err := objc.RegisterClass("YZBluetoothDelegate", objc.GetClass("NSObject"), protocols, nil, methods)
	if err != nil {
		return fmt.Errorf("corebluetooth: register delegate class: %w", err)
	}
	clsDelegate = cls
	return nil
}

var selValue = objc.RegisterName("value")

func parseAdvertisement(adv objc.ID) Advertisement {
	if adv == 0 {
		return Advertisement{}
	}
	a := Advertisement{LocalName: goString(adv.Send(selObjectForKey, nsString(advLocalNameKey)))}
	for _, u := range arrayItems(adv.Send(selObjectForKey, nsString(advServiceUUIDsKey))) {
		a.ServiceUUIDs = append(a.ServiceUUIDs, uuidString(u))
	}
	return a
}
