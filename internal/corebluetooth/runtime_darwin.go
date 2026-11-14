// SPDX-License-Identifier: GPL-3.0-only

package corebluetooth

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Native interop without cgo, following MyGo's rules: every function and
// callback is bound once at load time, and Go never blocks the queue that
// CoreBluetooth delivers on.

var (
	loadOnce sync.Once
	loadErr  error

	// objc_msgSend bound once per return type. objc.Send[T] would rebuild
	// the function with reflection on every call.
	sendPtr  func(id objc.ID, sel objc.SEL) unsafe.Pointer
	sendUint func(id objc.ID, sel objc.SEL) uint
	sendInt  func(id objc.ID, sel objc.SEL) int
	sendBool func(id objc.ID, sel objc.SEL) bool

	poolPush func() uintptr
	poolPop  func(uintptr)

	dispatchQueueCreate func(label string, attr uintptr) uintptr
	dispatchAttrFreq    func(attr uintptr, freq uintptr) uintptr
	dispatchAsyncF      func(queue, ctx, work uintptr)

	// trampoline is the single purego callback that runs Go functions on a
	// dispatch queue; ctx is a key into pendingWork.
	trampoline  uintptr
	pendingWork sync.Map // uintptr -> func()
	nextWork    atomic.Uintptr
)

const (
	rtldFlags = purego.RTLD_NOW | purego.RTLD_GLOBAL
	// DISPATCH_AUTORELEASE_FREQUENCY_WORK_ITEM: drain an autorelease pool
	// around every block, so autoreleased objects created on the queue
	// (including inside delegate callbacks) do not pile up.
	autoreleaseWorkItem = 1
)

var (
	clsCBCentralManager objc.Class
	clsCBUUID           objc.Class
	clsNSUUID           objc.Class
	clsNSString         objc.Class
	clsNSData           objc.Class
	clsNSMutableArray   objc.Class

	selAlloc                 = objc.RegisterName("alloc")
	selNew                   = objc.RegisterName("new")
	selRetain                = objc.RegisterName("retain")
	selRelease               = objc.RegisterName("release")
	selCount                 = objc.RegisterName("count")
	selObjectAtIndex         = objc.RegisterName("objectAtIndex:")
	selObjectForKey          = objc.RegisterName("objectForKey:")
	selArrayWithCapacity     = objc.RegisterName("arrayWithCapacity:")
	selAddObject             = objc.RegisterName("addObject:")
	selStringWithUTF8String  = objc.RegisterName("stringWithUTF8String:")
	selUTF8String            = objc.RegisterName("UTF8String")
	selDataWithBytesLength   = objc.RegisterName("dataWithBytes:length:")
	selBytes                 = objc.RegisterName("bytes")
	selLength                = objc.RegisterName("length")
	selIntValue              = objc.RegisterName("intValue")
	selUUIDWithString        = objc.RegisterName("UUIDWithString:")
	selInitWithUUIDString    = objc.RegisterName("initWithUUIDString:")
	selUUIDString            = objc.RegisterName("UUIDString")
	selDomain                = objc.RegisterName("domain")
	selCode                  = objc.RegisterName("code")
	selLocalizedDescription  = objc.RegisterName("localizedDescription")
	selInitWithDelegateQueue = objc.RegisterName("initWithDelegate:queue:options:")
	selAuthorization         = objc.RegisterName("authorization")
)

func load() error {
	loadOnce.Do(func() { loadErr = doLoad() })
	return loadErr
}

func doLoad() error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/CoreBluetooth.framework/CoreBluetooth", rtldFlags); err != nil {
		return fmt.Errorf("corebluetooth: load framework: %w", err)
	}
	libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", rtldFlags)
	if err != nil {
		return fmt.Errorf("corebluetooth: load libobjc: %w", err)
	}
	msgSend, err := purego.Dlsym(libobjc, "objc_msgSend")
	if err != nil {
		return fmt.Errorf("corebluetooth: %w", err)
	}
	purego.RegisterFunc(&sendPtr, msgSend)
	purego.RegisterFunc(&sendUint, msgSend)
	purego.RegisterFunc(&sendInt, msgSend)
	purego.RegisterFunc(&sendBool, msgSend)
	purego.RegisterLibFunc(&poolPush, libobjc, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&poolPop, libobjc, "objc_autoreleasePoolPop")

	libSystem, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", rtldFlags)
	if err != nil {
		return fmt.Errorf("corebluetooth: load libSystem: %w", err)
	}
	purego.RegisterLibFunc(&dispatchQueueCreate, libSystem, "dispatch_queue_create")
	purego.RegisterLibFunc(&dispatchAttrFreq, libSystem, "dispatch_queue_attr_make_with_autorelease_frequency")
	purego.RegisterLibFunc(&dispatchAsyncF, libSystem, "dispatch_async_f")
	trampoline = purego.NewCallback(runWork)

	classes := map[string]*objc.Class{
		"CBCentralManager": &clsCBCentralManager,
		"CBUUID":           &clsCBUUID,
		"NSUUID":           &clsNSUUID,
		"NSString":         &clsNSString,
		"NSData":           &clsNSData,
		"NSMutableArray":   &clsNSMutableArray,
	}
	for name, dst := range classes {
		if *dst = objc.GetClass(name); *dst == 0 {
			return fmt.Errorf("corebluetooth: class %s not found", name)
		}
	}
	return registerDelegateClass()
}

// newSerialQueue returns a serial dispatch queue that drains an autorelease
// pool after every work item. Queues are never freed; create one per Central.
func newSerialQueue(label string) uintptr {
	return dispatchQueueCreate(label, dispatchAttrFreq(0, autoreleaseWorkItem))
}

func dispatchAsync(queue uintptr, f func()) {
	key := nextWork.Add(1)
	pendingWork.Store(key, f)
	dispatchAsyncF(queue, key, trampoline)
}

func runWork(ctx uintptr) {
	f, ok := pendingWork.LoadAndDelete(ctx)
	if ok {
		f.(func())()
	}
}

// withPool runs f inside an autorelease pool on one OS thread. Use it for
// Objective-C calls made outside a dispatch queue.
func withPool(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p := poolPush()
	defer poolPop(p)
	f()
}

// nsString returns an autoreleased NSString.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	var pin runtime.Pinner
	pin.Pin(&b[0])
	defer pin.Unpin()
	return objc.ID(clsNSString).Send(selStringWithUTF8String, unsafe.Pointer(&b[0]))
}

// goString copies an NSString; nil gives "".
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := (*byte)(sendPtr(s, selUTF8String))
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// nsData returns an autoreleased copy of b.
func nsData(b []byte) objc.ID {
	if len(b) == 0 {
		return objc.ID(clsNSData).Send(selDataWithBytesLength, uintptr(0), uint(0))
	}
	var pin runtime.Pinner
	pin.Pin(&b[0])
	defer pin.Unpin()
	return objc.ID(clsNSData).Send(selDataWithBytesLength, unsafe.Pointer(&b[0]), uint(len(b)))
}

// goBytes copies an NSData; nil gives nil.
func goBytes(d objc.ID) []byte {
	if d == 0 {
		return nil
	}
	n := sendUint(d, selLength)
	if n == 0 {
		return []byte{}
	}
	p := (*byte)(sendPtr(d, selBytes))
	return append([]byte(nil), unsafe.Slice(p, n)...)
}

// arrayItems lists an NSArray; nil gives nil.
func arrayItems(a objc.ID) []objc.ID {
	if a == 0 {
		return nil
	}
	n := sendUint(a, selCount)
	items := make([]objc.ID, 0, n)
	for i := uint(0); i < n; i++ {
		items = append(items, a.Send(selObjectAtIndex, i))
	}
	return items
}

// cbUUIDs returns an autoreleased NSArray of CBUUID, or nil (meaning "any")
// when uuids is empty.
func cbUUIDs(uuids []string) objc.ID {
	if len(uuids) == 0 {
		return 0
	}
	arr := objc.ID(clsNSMutableArray).Send(selArrayWithCapacity, uint(len(uuids)))
	for _, u := range uuids {
		arr.Send(selAddObject, objc.ID(clsCBUUID).Send(selUUIDWithString, nsString(u)))
	}
	return arr
}

// nsUUIDs returns an autoreleased NSArray of NSUUID. Invalid strings are
// skipped.
func nsUUIDs(ids []string) objc.ID {
	arr := objc.ID(clsNSMutableArray).Send(selArrayWithCapacity, uint(len(ids)))
	for _, s := range ids {
		u := objc.ID(clsNSUUID).Send(selAlloc).Send(selInitWithUUIDString, nsString(s))
		if u == 0 {
			continue
		}
		arr.Send(selAddObject, u)
		u.Send(selRelease)
	}
	return arr
}

// uuidString returns the UUIDString of a CBUUID or NSUUID.
func uuidString(u objc.ID) string {
	if u == 0 {
		return ""
	}
	return goString(u.Send(selUUIDString))
}

// Error is an NSError reported by CoreBluetooth.
type Error struct {
	Domain      string
	Code        int
	Description string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (%s %d)", e.Description, e.Domain, e.Code)
}

// goError converts an NSError; nil gives nil.
func goError(e objc.ID) error {
	if e == 0 {
		return nil
	}
	return &Error{
		Domain:      goString(e.Send(selDomain)),
		Code:        sendInt(e, selCode),
		Description: goString(e.Send(selLocalizedDescription)),
	}
}
