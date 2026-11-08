// SPDX-License-Identifier: GPL-3.0-only

package macinput

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"

	"github.com/zJiaJun/yaozhi/internal/cf"
)

// Usage is a HID usage as UserKeyMapping encodes it: page<<32 | usage.
type Usage uint64

const (
	// UsageNone as a mapping destination disables the source key.
	UsageNone Usage = 0
	// UsageF5 is keyboard F5 (page 0x07, usage 0x3E), what RC003's voice key
	// sends (open-voice-bridge RemoteButtons.swift, remote-mic-app
	// RemoteVoiceFunctionMapper.swift).
	UsageF5 Usage = 0x7_0000_003E
)

// KeyMapping is one entry of a HID service's UserKeyMapping.
type KeyMapping struct {
	Src, Dst Usage
}

const (
	keyMappingProperty = "UserKeyMapping"
	keyMappingSrc      = "HIDKeyboardModifierMappingSrc"
	keyMappingDst      = "HIDKeyboardModifierMappingDst"
)

// ErrNoService reports that no HID service matched.
var ErrNoService = errors.New("macinput: no matching HID service")

var (
	iokitOnce sync.Once
	iokitErr  error

	hidClientCreate      func(alloc uintptr) cf.Ref
	hidClientCopyService func(client cf.Ref) cf.Ref
	hidServiceCopyProp   func(service cf.Ref, key cf.Ref) cf.Ref
	hidServiceSetProp    func(service cf.Ref, key cf.Ref, value cf.Ref) bool
	hidServiceRegistryID func(service cf.Ref) cf.Ref
)

func loadIOKit() error {
	iokitOnce.Do(func() {
		if iokitErr = cf.Load(); iokitErr != nil {
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			iokitErr = fmt.Errorf("macinput: load IOKit: %w", err)
			return
		}
		purego.RegisterLibFunc(&hidClientCreate, lib, "IOHIDEventSystemClientCreateSimpleClient")
		purego.RegisterLibFunc(&hidClientCopyService, lib, "IOHIDEventSystemClientCopyServices")
		purego.RegisterLibFunc(&hidServiceCopyProp, lib, "IOHIDServiceClientCopyProperty")
		purego.RegisterLibFunc(&hidServiceSetProp, lib, "IOHIDServiceClientSetProperty")
		purego.RegisterLibFunc(&hidServiceRegistryID, lib, "IOHIDServiceClientGetRegistryID")
	})
	return iokitErr
}

// HIDServices is a snapshot of the HID event services of one device. Close
// it when done.
type HIDServices struct {
	client   cf.Ref
	services cf.Ref // the copied array; keeps each service alive
	Items    []HIDService
}

// HIDService is one HID event service (one per top-level collection).
type HIDService struct {
	ref        cf.Ref
	RegistryID uint64
}

// ServicesFor returns the HID event services whose VendorID and ProductID
// match. It finds none while the device is disconnected.
func ServicesFor(vendorID, productID int) (*HIDServices, error) {
	if err := loadIOKit(); err != nil {
		return nil, err
	}
	s := &HIDServices{client: hidClientCreate(0)}
	if s.client == 0 {
		return nil, errors.New("macinput: IOHIDEventSystemClientCreateSimpleClient failed")
	}
	s.services = hidClientCopyService(s.client)
	for _, svc := range cf.Items(s.services) {
		if intProperty(svc, "VendorID") != int64(vendorID) || intProperty(svc, "ProductID") != int64(productID) {
			continue
		}
		id, _ := cf.Int64(hidServiceRegistryID(svc))
		s.Items = append(s.Items, HIDService{ref: svc, RegistryID: uint64(id)})
	}
	return s, nil
}

// Close releases the snapshot.
func (s *HIDServices) Close() {
	cf.Release(s.services)
	cf.Release(s.client)
	s.Items = nil
}

func intProperty(svc cf.Ref, key string) int64 {
	k := cf.String(key)
	defer cf.Release(k)
	v := hidServiceCopyProp(svc, k)
	defer cf.Release(v)
	n, ok := cf.Int64(v)
	if !ok {
		return -1
	}
	return n
}

// KeyMappings reads the service's UserKeyMapping.
func (s HIDService) KeyMappings() []KeyMapping {
	k := cf.String(keyMappingProperty)
	defer cf.Release(k)
	arr := hidServiceCopyProp(s.ref, k)
	defer cf.Release(arr)
	var out []KeyMapping
	for _, d := range cf.Items(arr) {
		src, ok1 := cf.Int64(cf.Get(d, keyMappingSrc))
		dst, ok2 := cf.Int64(cf.Get(d, keyMappingDst))
		if ok1 && ok2 {
			out = append(out, KeyMapping{Usage(src), Usage(dst)})
		}
	}
	return out
}

// SetKeyMappings replaces the service's UserKeyMapping. An empty list clears
// it. macOS forgets it when the device reconnects.
func (s HIDService) SetKeyMappings(ms []KeyMapping) error {
	srcKey, dstKey := cf.String(keyMappingSrc), cf.String(keyMappingDst)
	defer cf.Release(srcKey)
	defer cf.Release(dstKey)
	dicts := make([]cf.Ref, len(ms))
	for i, m := range ms {
		src, dst := cf.Number(int64(m.Src)), cf.Number(int64(m.Dst))
		dicts[i] = cf.Dictionary([]cf.Ref{srcKey, dstKey}, []cf.Ref{src, dst})
		cf.Release(src)
		cf.Release(dst)
	}
	arr := cf.Array(dicts)
	for _, d := range dicts {
		cf.Release(d)
	}
	defer cf.Release(arr)
	k := cf.String(keyMappingProperty)
	defer cf.Release(k)
	if !hidServiceSetProp(s.ref, k, arr) {
		return fmt.Errorf("macinput: set UserKeyMapping on service %#x failed", s.RegistryID)
	}
	return nil
}

// withMapping returns ms with src mapped to dst, replacing any mapping of src
// and keeping the others in order.
func withMapping(ms []KeyMapping, src, dst Usage) []KeyMapping {
	out := withoutMapping(ms, src)
	return append(out, KeyMapping{src, dst})
}

// withoutMapping returns ms without any mapping of src.
func withoutMapping(ms []KeyMapping, src Usage) []KeyMapping {
	out := make([]KeyMapping, 0, len(ms))
	for _, m := range ms {
		if m.Src != src {
			out = append(out, m)
		}
	}
	return out
}

// lookup returns the mapping of src in ms.
func lookup(ms []KeyMapping, src Usage) (KeyMapping, bool) {
	for _, m := range ms {
		if m.Src == src {
			return m, true
		}
	}
	return KeyMapping{}, false
}

// RemapKey maps key src to dst on every HID service of the device and
// returns how many services it changed and a function that puts back what
// each one had for src, leaving its other mappings, including ones others
// added meanwhile, alone. If writing any service fails, the ones already
// changed are restored and the error returned.
func RemapKey(vendorID, productID int, src, dst Usage) (n int, restore func() error, err error) {
	svcs, err := ServicesFor(vendorID, productID)
	if err != nil {
		return 0, nil, err
	}
	if len(svcs.Items) == 0 {
		svcs.Close()
		return 0, nil, ErrNoService
	}
	type saved struct {
		svc  HIDService
		prev KeyMapping
		had  bool
	}
	var done []saved
	undo := func() error {
		var errs []error
		for _, s := range done {
			cur := s.svc.KeyMappings()
			next := withoutMapping(cur, src)
			if s.had {
				next = withMapping(cur, src, s.prev.Dst)
			}
			if err := s.svc.SetKeyMappings(next); err != nil {
				errs = append(errs, err)
			}
		}
		svcs.Close()
		return errors.Join(errs...)
	}
	for _, svc := range svcs.Items {
		cur := svc.KeyMappings()
		prev, had := lookup(cur, src)
		if err := svc.SetKeyMappings(withMapping(cur, src, dst)); err != nil {
			return 0, nil, errors.Join(err, undo())
		}
		done = append(done, saved{svc, prev, had})
	}
	var once sync.Once
	var restoreErr error
	return len(done), func() error {
		once.Do(func() { restoreErr = undo() })
		return restoreErr
	}, nil
}
