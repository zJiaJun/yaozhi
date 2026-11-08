// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package atvvble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zJiaJun/yaozhi/internal/atvv"
	cb "github.com/zJiaJun/yaozhi/internal/corebluetooth"
	"github.com/zJiaJun/yaozhi/internal/device"
)

var (
	// ErrUnauthorized: the app may not use Bluetooth. The user has to allow
	// it in System Settings → Privacy & Security → Bluetooth.
	ErrUnauthorized = errors.New("atvvble: no Bluetooth permission")
	// ErrUnsupported: this Mac has no Bluetooth LE.
	ErrUnsupported = errors.New("atvvble: Bluetooth LE is not supported")
)

// Adapter finds and keeps connected one ATVV remote. The process must run
// inside an app bundle with NSBluetoothAlwaysUsageDescription, or macOS kills
// it on first Bluetooth use.
type Adapter struct {
	descs []device.Descriptor

	// AcceptAny also accepts ATVV remotes no descriptor matches, reported
	// with a nil Device. For trying out new remotes.
	AcceptAny bool
	// Logf receives diagnostics (never audio). nil discards them.
	Logf func(format string, args ...any)
	// PollInterval is how often to look for a remote while none is
	// connected.
	PollInterval time.Duration
	// InitTimeout bounds the ATVV setup after connecting; on timeout the
	// adapter drops the connection and tries again.
	InitTimeout time.Duration
}

var _ device.Device = (*Adapter)(nil)

// New returns an adapter for the descriptors whose voice protocol is ATVV.
func New(descs []device.Descriptor) *Adapter {
	a := &Adapter{PollInterval: 2 * time.Second, InitTimeout: 10 * time.Second}
	for _, d := range descs {
		if d.Voice.Protocol == device.ProtocolATVV {
			a.descs = append(a.descs, d)
		}
	}
	return a
}

// Run implements device.Device. A remote is only found once the system has
// it connected: pair it in System Settings → Bluetooth first (its keys need
// that anyway). A paired remote that went to sleep reconnects when any key is
// pressed, and Run picks it up again.
func (a *Adapter) Run(ctx context.Context, emit func(device.Event)) error {
	logf := a.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	r := &runner{a: a, emit: emit, logf: logf, rejected: map[string]bool{}, seen: map[string]bool{}}
	fatal := make(chan error, 1)
	r.fatal = func(err error) {
		select {
		case fatal <- err:
		default:
		}
	}

	c, err := cb.NewCentral(&cb.Delegate{
		StateChanged:       r.guard1(r.stateChanged),
		Connected:          func(p cb.Peripheral) { r.live(func() { r.connected(p) }) },
		FailedToConnect:    func(p cb.Peripheral, err error) { r.live(func() { r.failedToConnect(p, err) }) },
		Disconnected:       func(p cb.Peripheral, err error) { r.live(func() { r.disconnected(p, err) }) },
		ServicesDiscovered: func(p cb.Peripheral, err error) { r.live(func() { r.servicesDiscovered(p, err) }) },
		CharacteristicsDiscovered: func(p cb.Peripheral, s cb.Service, err error) {
			r.live(func() { r.characteristicsDiscovered(p, s, err) })
		},
		NotifyStateUpdated: func(p cb.Peripheral, ch cb.Characteristic, err error) {
			r.live(func() { r.notifyStateUpdated(p, ch, err) })
		},
		ValueUpdated: func(p cb.Peripheral, ch cb.Characteristic, v []byte, err error) {
			r.live(func() { r.valueUpdated(p, ch, v, err) })
		},
	})
	if err != nil {
		return err
	}
	r.c = c

	poll := time.NewTicker(a.PollInterval)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			c.Do(func() { r.live(r.poll) })
			continue
		case err = <-fatal:
		case <-ctx.Done():
		}
		break
	}

	done := make(chan struct{})
	c.Do(func() {
		r.shutdown()
		c.Close()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		logf("关闭蓝牙连接超时")
	}
	return err
}

// runner is the state of one Run. Every field is used only on the Central's
// queue.
type runner struct {
	a         *Adapter
	c         *cb.Central
	emit      func(device.Event)
	logf      func(string, ...any)
	fatal     func(error)
	stopped   bool
	poweredOn bool

	state device.State
	desc  *device.Descriptor // nil for an unknown remote (AcceptAny)
	name  string

	// target is the remote being connected or connected, or after a
	// disconnect the one with a pending reconnect.
	target  *cb.Peripheral
	link    *link // non-nil while connected
	attempt int   // invalidates init timers of earlier connections
	tx      *cb.Characteristic
	audio   *cb.Characteristic
	control *cb.Characteristic
	subs    map[string]bool

	rejected map[string]bool // identifiers of remotes that cannot be used
	seen     map[string]bool // unmatched remotes already logged
}

func (r *runner) live(f func()) {
	if !r.stopped {
		f()
	}
}

func (r *runner) guard1(f func(cb.State)) func(cb.State) {
	return func(s cb.State) { r.live(func() { f(s) }) }
}

func (r *runner) setState(s device.State, err error) {
	r.state = s
	r.emit(device.StateChanged{State: s, Device: r.desc, Name: r.name, Err: err})
}

func (r *runner) stateChanged(s cb.State) {
	r.logf("蓝牙状态：%s", s)
	switch s {
	case cb.StatePoweredOn:
		r.poweredOn = true
		r.desc, r.name = nil, ""
		r.setState(device.StateSearching, nil)
		r.find()
	case cb.StatePoweredOff, cb.StateResetting:
		// Connections are gone without disconnect callbacks.
		r.poweredOn = false
		r.closeLink(false)
		r.target = nil
		r.setState(device.StateUnavailable, errors.New("蓝牙已关闭"))
	case cb.StateUnauthorized:
		r.fatal(ErrUnauthorized)
	case cb.StateUnsupported:
		r.fatal(ErrUnsupported)
	}
}

func (r *runner) poll() {
	if r.poweredOn && r.state == device.StateSearching {
		r.find()
	}
}

// find connects to the first acceptable remote the system has connected.
func (r *runner) find() {
	for _, p := range r.c.RetrieveConnected(atvv.ServiceUUID) {
		id, name := p.Identifier(), p.Name()
		if r.rejected[id] {
			continue
		}
		desc := r.match(name)
		if desc == nil && !r.a.AcceptAny {
			if !r.seen[id] {
				r.seen[id] = true
				r.logf("忽略不认识的 ATVV 设备 %q（%s）", name, id)
			}
			continue
		}
		r.connect(p, desc)
		return
	}
}

func (r *runner) match(name string) *device.Descriptor {
	for i := range r.a.descs {
		if r.a.descs[i].MatchesName(name) {
			return &r.a.descs[i]
		}
	}
	return nil
}

func (r *runner) connect(p cb.Peripheral, desc *device.Descriptor) {
	if r.target != nil {
		if r.target.Equal(p) {
			return // already connecting; the pending connect completes
		}
		r.c.CancelConnect(*r.target)
	}
	r.target, r.desc, r.name = &p, desc, p.Name()
	r.logf("连接 %q（%s）", r.name, p.Identifier())
	r.c.Connect(p)
}

func (r *runner) isTarget(p cb.Peripheral) bool { return r.target != nil && r.target.Equal(p) }

func (r *runner) connected(p cb.Peripheral) {
	if !r.isTarget(p) {
		return
	}
	r.attempt++
	r.tx, r.audio, r.control = nil, nil, nil
	r.subs = map[string]bool{}
	r.link = newLink(r.emit, r.write, r.logf)
	r.setState(device.StateConnecting, nil)
	p.DiscoverServices(atvv.ServiceUUID)

	attempt := r.attempt
	time.AfterFunc(r.a.InitTimeout, func() {
		r.c.Do(func() {
			r.live(func() {
				if r.attempt == attempt && r.state == device.StateConnecting {
					r.giveUp(fmt.Errorf("%v 内没完成 ATVV 初始化", r.a.InitTimeout), false)
				}
			})
		})
	})
}

func (r *runner) failedToConnect(p cb.Peripheral, err error) {
	if !r.isTarget(p) {
		return
	}
	r.logf("连接 %q 失败：%v", r.name, err)
	r.target = nil
	r.setState(device.StateSearching, err)
}

// disconnected handles the remote going away (asleep, out of range, turned
// off). A pending connect is left in place so that it reconnects as soon as
// the system does.
func (r *runner) disconnected(p cb.Peripheral, err error) {
	if !r.isTarget(p) {
		return
	}
	if err == nil {
		err = errors.New("连接已断开")
	}
	r.logf("%q 断开：%v", r.name, err)
	r.closeLink(false)
	r.setState(device.StateSearching, err)
	r.c.Connect(p)
}

// giveUp drops the current connection. reject also stops the adapter from
// trying this remote again until the next Run.
func (r *runner) giveUp(err error, reject bool) {
	if r.target == nil {
		return
	}
	p := *r.target
	r.logf("放弃 %q：%v", r.name, err)
	if reject {
		r.rejected[p.Identifier()] = true
	}
	r.closeLink(true)
	r.target = nil
	r.c.CancelConnect(p) // the system keeps its own link for the keys
	r.setState(device.StateSearching, err)
}

// closeLink ends an open voice stream and forgets the connection's ATVV
// state. closeMic asks the remote to stop streaming, possible only while the
// link is still up.
func (r *runner) closeLink(closeMic bool) {
	if r.link != nil {
		r.link.end(closeMic && r.tx != nil)
	}
	r.link = nil
	r.tx, r.audio, r.control = nil, nil, nil
	r.attempt++
}

func (r *runner) shutdown() {
	r.closeLink(true)
	r.stopped = true
}

func (r *runner) servicesDiscovered(p cb.Peripheral, err error) {
	if !r.isTarget(p) || r.link == nil {
		return
	}
	if err != nil {
		r.giveUp(fmt.Errorf("查找服务失败：%w", err), false)
		return
	}
	for _, s := range p.Services() {
		if strings.EqualFold(s.UUID(), atvv.ServiceUUID) {
			p.DiscoverCharacteristics(s, atvv.TransmitUUID, atvv.AudioUUID, atvv.ControlUUID)
			return
		}
	}
	r.giveUp(errors.New("设备没有 ATVV 语音服务"), true)
}

func (r *runner) characteristicsDiscovered(p cb.Peripheral, s cb.Service, err error) {
	if !r.isTarget(p) || r.link == nil {
		return
	}
	if err != nil {
		r.giveUp(fmt.Errorf("查找 ATVV 特征失败：%w", err), false)
		return
	}
	for _, ch := range s.Characteristics() {
		switch u := ch.UUID(); {
		case strings.EqualFold(u, atvv.TransmitUUID):
			r.tx = &ch
		case strings.EqualFold(u, atvv.AudioUUID):
			r.audio = &ch
			p.SetNotify(ch, true)
		case strings.EqualFold(u, atvv.ControlUUID):
			r.control = &ch
			p.SetNotify(ch, true)
		}
	}
	if r.tx == nil || r.audio == nil || r.control == nil {
		r.giveUp(errors.New("ATVV 特征不完整"), true)
	}
}

func (r *runner) notifyStateUpdated(p cb.Peripheral, ch cb.Characteristic, err error) {
	if !r.isTarget(p) || r.link == nil {
		return
	}
	if err == nil && !ch.IsNotifying() {
		err = errors.New("未生效")
	}
	if err != nil {
		r.giveUp(fmt.Errorf("订阅 %s 失败：%w", ch.UUID(), err), false)
		return
	}
	r.subs[strings.ToUpper(ch.UUID())] = true
	if r.subs[atvv.AudioUUID] && r.subs[atvv.ControlUUID] {
		r.write(atvv.GetCapabilities())
	}
}

func (r *runner) write(b []byte) {
	if r.tx == nil || r.target == nil {
		return
	}
	withResponse := r.tx.Properties()&cb.PropWriteWithoutResponse == 0
	r.target.Write(*r.tx, b, withResponse)
}

func (r *runner) valueUpdated(p cb.Peripheral, ch cb.Characteristic, v []byte, err error) {
	if !r.isTarget(p) || r.link == nil {
		return
	}
	if err != nil {
		r.logf("读取 %s 出错：%v", ch.UUID(), err)
		return
	}
	switch u := ch.UUID(); {
	case strings.EqualFold(u, atvv.ControlUUID):
		ready, err := r.link.control(v)
		if err != nil {
			r.giveUp(err, true)
			return
		}
		if ready && r.state != device.StateReady {
			r.setState(device.StateReady, nil)
		}
	case strings.EqualFold(u, atvv.AudioUUID):
		r.link.audio(v)
	}
}
