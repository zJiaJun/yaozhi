// SPDX-License-Identifier: GPL-3.0-only

package coreaudio

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

// Aggregate devices: a device made of other devices, as Audio MIDI Setup's
// "Create Aggregate Device" makes them. Yaozhi makes one holding only
// BlackHole 2ch, so that input methods which hide virtual devices (Typeless
// drops names ending in "(Virtual)") list it as a microphone. BlackHole itself
// is not touched.

var (
	selTransportType   = fourCC("tran") // kAudioDevicePropertyTransportType
	selFullSubDevices  = fourCC("grup") // kAudioAggregateDevicePropertyFullSubDeviceList
	transportAggregate = fourCC("grup") // kAudioDeviceTransportTypeAggregate

	createAggregate  func(desc cf.Ref, out *uint32) int32
	destroyAggregate func(id uint32) int32
)

// Keys of the description AudioHardwareCreateAggregateDevice takes
// (AudioHardware.h, kAudioAggregateDevice…Key).
const (
	keyUID        = "uid"
	keyName       = "name"
	keySubDevices = "subdevices"
	keyMain       = "master" // the clock source; the header still spells it so
	keyPrivate    = "private"
	keyStacked    = "stacked"
)

// ErrNotAggregate reports a device that exists but is not an aggregate that
// this package made.
var ErrNotAggregate = errors.New("coreaudio: not an aggregate device")

func uint32Property(obj, sel uint32) (uint32, bool) {
	addr := propertyAddress{sel, scopeGlobal, elementMain}
	var v uint32
	size := uint32(unsafe.Sizeof(v))
	if getPropertyData(obj, &addr, 0, nil, &size, unsafe.Pointer(&v)) != 0 {
		return 0, false
	}
	return v, true
}

// subDevices lists the UIDs of an aggregate's devices, or nil.
func subDevices(id uint32) []string {
	addr := propertyAddress{selFullSubDevices, scopeGlobal, elementMain}
	var arr cf.Ref
	size := uint32(unsafe.Sizeof(arr))
	if getPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&arr)) != 0 || arr == 0 {
		return nil
	}
	defer cf.Release(arr)
	var uids []string
	for _, s := range cf.Items(arr) {
		uids = append(uids, cf.GoString(s))
	}
	return uids
}

// CreateAggregate makes a public aggregate device named name, with the
// given UID, holding the devices subUIDs (the first is the clock source). It
// stays until DestroyAggregate, or until the system forgets it; callers
// should check for it again rather than assume it lasts.
func CreateAggregate(name, uid string, subUIDs []string) (Device, error) {
	if err := load(); err != nil {
		return Device{}, err
	}
	if len(subUIDs) == 0 {
		return Device{}, errors.New("coreaudio: aggregate needs at least one device")
	}
	var owned []cf.Ref
	keep := func(r cf.Ref) cf.Ref { owned = append(owned, r); return r }
	defer func() {
		for _, r := range owned {
			cf.Release(r)
		}
	}()
	subs := make([]cf.Ref, len(subUIDs))
	for i, s := range subUIDs {
		subs[i] = keep(cf.Dictionary([]cf.Ref{keep(cf.String(keyUID))}, []cf.Ref{keep(cf.String(s))}))
	}
	desc := keep(cf.Dictionary(
		[]cf.Ref{
			keep(cf.String(keyUID)), keep(cf.String(keyName)), keep(cf.String(keySubDevices)),
			keep(cf.String(keyMain)), keep(cf.String(keyPrivate)), keep(cf.String(keyStacked)),
		},
		[]cf.Ref{
			keep(cf.String(uid)), keep(cf.String(name)), keep(cf.Array(subs)),
			keep(cf.String(subUIDs[0])), keep(cf.Number(0)), keep(cf.Number(0)),
		},
	))
	var id uint32
	if st := createAggregate(desc, &id); st != 0 {
		return Device{}, fmt.Errorf("coreaudio: create aggregate %q: OSStatus %d", name, st)
	}
	// The device shows up in the device list a moment after the call.
	for range 20 {
		if d, err := DeviceByUID(uid); err == nil {
			return d, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return describe(id)
}

// DestroyAggregate removes the aggregate device with the given UID. It does
// nothing when there is none, and refuses a device that is not an aggregate.
func DestroyAggregate(uid string) error {
	d, err := DeviceByUID(uid)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !d.Aggregate {
		return fmt.Errorf("%w: %s", ErrNotAggregate, uid)
	}
	if st := destroyAggregate(d.ID); st != 0 {
		return fmt.Errorf("coreaudio: destroy aggregate %q: OSStatus %d", uid, st)
	}
	return nil
}
