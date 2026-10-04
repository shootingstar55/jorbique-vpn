//go:build darwin

package applekit

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// CGFloat is the floating-point type used throughout AppKit geometry. On every
// 64-bit Apple platform (the only ones this client targets) it is a float64.
type CGFloat = float64

// Point mirrors CGPoint / NSPoint: two CGFloats, 16 bytes, passed in registers.
type Point struct {
	X, Y CGFloat
}

// Size mirrors CGSize / NSSize.
type Size struct {
	Width, Height CGFloat
}

// Rect mirrors CGRect / NSRect: 32 bytes, which on arm64 is passed indirectly
// (by hidden pointer) rather than in registers. This is the struct-by-value
// case the migration plan flags as the primary ABI risk; purego marshals it
// transparently through objc.Send, validated by the Phase 0 spike.
type Rect struct {
	Origin Point
	Size   Size
}

// Range mirrors NSRange (two NSUInteger fields, 16 bytes).
type Range struct {
	Location, Length uint
}

// NewRect is a convenience constructor matching the NSMakeRect argument order.
func NewRect(x, y, w, h CGFloat) Rect {
	return Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// Re-exported objc primitives so callers in this package and its siblings refer
// to a single bridge surface rather than importing purego/objc directly.
type (
	// ID is an Objective-C object pointer (id).
	ID = objc.ID
	// SEL is a selector.
	SEL = objc.SEL
)

// nilID is the typed zero object, equivalent to Objective-C nil.
const nilID = ID(0)

// selCache memoizes selector registration. sel_registerName is cheap, but the
// menu refresh path resolves the same handful of selectors repeatedly, and a
// map lookup is cheaper still while keeping call sites readable.
// sync.Map (key string, value SEL) so concurrent callers — the AppKit main
// thread and any background goroutine resolving a selector — never race on the
// cache. sel_registerName is idempotent, so a duplicate Store on first use is
// harmless.
var selCache sync.Map

// Sel returns the selector for name, registering it on first use.
func Sel(name string) SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

// Class looks up a registered Objective-C class by name. It returns the zero
// Class if the name is unknown (e.g. the owning framework was not loaded).
func Class(name string) objc.Class {
	return objc.GetClass(name)
}

// send is the workhorse for object messages whose return value is another
// object (id) — by far the most common shape. Typed wrappers for non-id returns
// (BOOL, NSInteger, struct) use the generic objc.Send[T] directly.
func send(id ID, sel string, args ...any) ID {
	return id.Send(Sel(sel), args...)
}

// cls sends a class message (e.g. +alloc, +sharedApplication) by treating the
// Class as the receiver id.
func cls(name, sel string, args ...any) ID {
	return ID(Class(name)).Send(Sel(sel), args...)
}

// intMsg sends a message returning an NSInteger.
func intMsg(id ID, sel string, args ...any) int {
	return objc.Send[int](id, Sel(sel), args...)
}

// nsArray builds an NSArray from the given objects via a mutable array, avoiding
// the nil-terminated +arrayWithObjects: variadic which objc_msgSend cannot
// express. Returns an empty (autoreleased) array when no objects are given.
func nsArray(objs ...ID) ID {
	arr := cls("NSMutableArray", "array")
	for _, o := range objs {
		arr.Send(Sel("addObject:"), o)
	}
	return arr
}

// nsString bridges a Go string to an autoreleased NSString. The byte slice is
// kept alive across the call by referencing its first element; the NSString
// copies the UTF-8 bytes, so the slice may be collected afterward. Receivers
// like -setTitle: copy or retain the NSString, so autorelease is safe here.
func nsString(s string) ID {
	b := append([]byte(s), 0)
	return cls("NSString", "stringWithUTF8String:", &b[0])
}

// goStringFromNS reads an NSString back into a Go string via -UTF8String.
func goStringFromNS(s ID) string {
	if s == nilID {
		return ""
	}
	// Take the result as a *byte rather than a uintptr so the C string is read
	// through a genuine pointer — converting an integer address back to an
	// unsafe.Pointer is exactly what go vet's unsafeptr check (correctly, in
	// general) refuses, and routing through *byte sidesteps it.
	return goCStr(objc.Send[*byte](s, Sel("UTF8String")))
}

// goCStr copies a NUL-terminated C string into a Go string. The buffer is owned
// by the (autoreleased) Objective-C object that produced it.
func goCStr(p *byte) string {
	if p == nil {
		return ""
	}
	var n int
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}
