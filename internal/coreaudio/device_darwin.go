// SPDX-License-Identifier: GPL-3.0-only

package coreaudio

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

// ErrNotFound reports that no audio device has the requested UID.
var ErrNotFound = errors.New("coreaudio: device not found")

// Device describes one Core Audio device.
type Device struct {
	ID             uint32
	UID            string
	Name           string
	SampleRate     float64 // nominal rate, as set in Audio MIDI Setup
	OutputChannels int
	InputChannels  int
	// Aggregate is true for an aggregate device; SubDevices then lists the
	// UIDs of the devices it holds.
	Aggregate  bool
	SubDevices []string
}

func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

var (
	selDevices      = fourCC("dev#")
	selDeviceUID    = fourCC("uid ")
	selName         = fourCC("lnam")
	selNominalRate  = fourCC("nsrt")
	selStreamConfig = fourCC("slay")
	scopeGlobal     = fourCC("glob")
	scopeOutput     = fourCC("outp")
	scopeInput      = fourCC("inpt")
	systemObject    = uint32(1)
	elementMain     = uint32(0)
	errNoProperty   = errors.New("coreaudio: property unavailable")
	loadOnce        sync.Once
	loadErr         error
	getPropertySize func(obj uint32, addr *propertyAddress, qualSize uint32, qual unsafe.Pointer, size *uint32) int32
	getPropertyData func(obj uint32, addr *propertyAddress, qualSize uint32, qual unsafe.Pointer, size *uint32, data unsafe.Pointer) int32
	audioToolboxLib uintptr
)

type propertyAddress struct {
	selector uint32
	scope    uint32
	element  uint32
}

func load() error {
	loadOnce.Do(func() { loadErr = doLoad() })
	return loadErr
}

func doLoad() error {
	const flags = purego.RTLD_NOW | purego.RTLD_GLOBAL
	ca, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", flags)
	if err != nil {
		return fmt.Errorf("coreaudio: load CoreAudio: %w", err)
	}
	purego.RegisterLibFunc(&getPropertySize, ca, "AudioObjectGetPropertyDataSize")
	purego.RegisterLibFunc(&getPropertyData, ca, "AudioObjectGetPropertyData")
	purego.RegisterLibFunc(&createAggregate, ca, "AudioHardwareCreateAggregateDevice")
	purego.RegisterLibFunc(&destroyAggregate, ca, "AudioHardwareDestroyAggregateDevice")

	if err := cf.Load(); err != nil {
		return err
	}
	audioToolboxLib, err = purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", flags)
	if err != nil {
		return fmt.Errorf("coreaudio: load AudioToolbox: %w", err)
	}
	return loadQueue()
}

func stringProperty(obj, sel uint32) (string, error) {
	addr := propertyAddress{sel, scopeGlobal, elementMain}
	var s cf.Ref
	size := uint32(unsafe.Sizeof(s))
	if st := getPropertyData(obj, &addr, 0, nil, &size, unsafe.Pointer(&s)); st != 0 {
		return "", fmt.Errorf("%w (%s: OSStatus %d)", errNoProperty, fourCCString(sel), st)
	}
	defer cf.Release(s) // the property returns a +1 reference
	return cf.GoString(s), nil
}

func channelCount(obj, scope uint32) int {
	addr := propertyAddress{selStreamConfig, scope, elementMain}
	var size uint32
	if getPropertySize(obj, &addr, 0, nil, &size) != 0 || size < 8 {
		return 0
	}
	buf := make([]byte, size)
	if getPropertyData(obj, &addr, 0, nil, &size, unsafe.Pointer(&buf[0])) != 0 {
		return 0
	}
	// AudioBufferList: UInt32 mNumberBuffers, then (after padding to 8)
	// AudioBuffer{UInt32 mNumberChannels; UInt32 mDataByteSize; void *mData}.
	n := int(*(*uint32)(unsafe.Pointer(&buf[0])))
	total := 0
	for i := 0; i < n && 8+16*i+4 <= len(buf); i++ {
		total += int(*(*uint32)(unsafe.Pointer(&buf[8+16*i])))
	}
	return total
}

func describe(id uint32) (Device, error) {
	uid, err := stringProperty(id, selDeviceUID)
	if err != nil {
		return Device{}, err
	}
	name, _ := stringProperty(id, selName)
	var rate float64
	addr := propertyAddress{selNominalRate, scopeGlobal, elementMain}
	size := uint32(unsafe.Sizeof(rate))
	getPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&rate))
	d := Device{
		ID:             id,
		UID:            uid,
		Name:           name,
		SampleRate:     rate,
		OutputChannels: channelCount(id, scopeOutput),
		InputChannels:  channelCount(id, scopeInput),
	}
	if t, ok := uint32Property(id, selTransportType); ok && t == transportAggregate {
		d.Aggregate = true
		d.SubDevices = subDevices(id)
	}
	return d, nil
}

// Devices lists every audio device the system knows.
func Devices() ([]Device, error) {
	if err := load(); err != nil {
		return nil, err
	}
	addr := propertyAddress{selDevices, scopeGlobal, elementMain}
	var size uint32
	if st := getPropertySize(systemObject, &addr, 0, nil, &size); st != 0 {
		return nil, fmt.Errorf("coreaudio: list devices: OSStatus %d", st)
	}
	if size == 0 {
		return nil, nil
	}
	ids := make([]uint32, size/4)
	if st := getPropertyData(systemObject, &addr, 0, nil, &size, unsafe.Pointer(&ids[0])); st != 0 {
		return nil, fmt.Errorf("coreaudio: list devices: OSStatus %d", st)
	}
	devs := make([]Device, 0, len(ids))
	for _, id := range ids[:size/4] {
		if d, err := describe(id); err == nil {
			devs = append(devs, d)
		}
	}
	return devs, nil
}

// DeviceByUID finds the device whose UID is uid.
func DeviceByUID(uid string) (Device, error) {
	devs, err := Devices()
	if err != nil {
		return Device{}, err
	}
	for _, d := range devs {
		if d.UID == uid {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("%w: %s", ErrNotFound, uid)
}

func fourCCString(v uint32) string {
	return string([]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
