//go:build darwin

package applekit

import (
	"testing"

	"github.com/ebitengine/purego/objc"
)

// These tests are the Phase 0 go/no-go gate from the migration plan. They prove
// the two ABI mechanisms every later phase depends on, using purego against the
// real system frameworks — so they only run on macOS, built with CGO_ENABLED=0.
//
// They deliberately avoid NSApplication / the run loop / any window-server
// interaction so they pass headlessly in `go test` (and so CI could run them on
// any reachable Mac). The full window/menu UI is smoke-tested manually by running
// the menu-bar app itself.

func TestSpikeLoadFrameworks(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Every handle must be non-zero, otherwise a later objc call into that
	// framework's classes would crash rather than error.
	for _, fw := range cabiFrameworks {
		if *fw.target == 0 {
			t.Errorf("framework %s did not load (handle is 0)", fw.name)
		}
	}
	if appKit == 0 {
		t.Error("AppKit did not load (handle is 0)")
	}
}

func TestSpikeBasicMessaging(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	const want = "purego round-trip"
	cstr := append([]byte(want), 0)

	// [[NSString alloc] initWithUTF8String:cstr] — 0-arg alloc, 1-arg init.
	str := cls("NSString", "alloc").Send(Sel("initWithUTF8String:"), &cstr[0])
	if str == nilID {
		t.Fatal("initWithUTF8String: returned nil")
	}

	// -length returns NSUInteger; assert the typed struct-free return path.
	if got := objc.Send[uint](str, Sel("length")); got != uint(len(want)) {
		t.Errorf("length = %d, want %d", got, len(want))
	}

	// -UTF8String returns a C string pointer; read it back and compare.
	if got := goStringFromNS(str); got != want {
		t.Errorf("UTF8String = %q, want %q", got, want)
	}
}

// TestSpikeStructByValueArgAndReturn is the primary ABI risk from the plan:
// passing a 32-byte NSRect by value as an argument and getting one back. On
// arm64 a 32-byte struct is passed indirectly (hidden pointer), the exact
// mechanism NSWindow initWithContentRect:... relies on. NSValue is used instead
// of NSWindow so the test needs no main thread or window server.
func TestSpikeStructByValueArgAndReturn(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	in := NewRect(12.5, 34.25, 640, 480.75)

	// +[NSValue valueWithRect:] takes the NSRect by value (the arg case)...
	val := cls("NSValue", "valueWithRect:", in)
	if val == nilID {
		t.Fatal("valueWithRect: returned nil")
	}
	// ...and -rectValue returns one by value (the return case).
	out := objc.Send[Rect](val, Sel("rectValue"))
	if out != in {
		t.Fatalf("NSRect did not round-trip: got %+v, want %+v", out, in)
	}

	// Also exercise the <=16-byte register-passed path (NSRange) for contrast.
	rng := Range{Location: 7, Length: 19}
	rv := cls("NSValue", "valueWithRange:", rng)
	if got := objc.Send[Range](rv, Sel("rangeValue")); got != rng {
		t.Fatalf("NSRange did not round-trip: got %+v, want %+v", got, rng)
	}
}

// TestSpikeNSPointFPRegisters covers the 16-byte homogeneous-float aggregate
// (NSPoint = two CGFloats) passed and returned by value. Unlike NSRange — also
// 16 bytes, but two integers in general-purpose registers — NSPoint travels in
// the SIMD/floating-point registers on arm64, a distinct ABI path that every
// frame/origin geometry call (SetFrame, Center, …) depends on.
func TestSpikeNSPointFPRegisters(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	in := Point{X: 12.5, Y: -34.25}
	// +[NSValue valueWithPoint:] takes the NSPoint by value (arg case)...
	val := cls("NSValue", "valueWithPoint:", in)
	if val == nilID {
		t.Fatal("valueWithPoint: returned nil")
	}
	// ...and -pointValue returns one by value (return case).
	if got := objc.Send[Point](val, Sel("pointValue")); got != in {
		t.Fatalf("NSPoint did not round-trip: got %+v, want %+v", got, in)
	}
}

// TestSpikeDynamicClassWithGoIMP is the second ABI risk: creating an
// Objective-C class at runtime whose method is implemented by a Go function,
// then messaging it. Every interactive panel needs this (button target/action,
// window delegates). It proves self/_cmd arrive correctly, an integer argument
// marshals in, and a return value marshals out.
func TestSpikeDynamicClassWithGoIMP(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var observed struct {
		called bool
		gotArg int
	}
	doubleAdd := Sel("akSpikeDoubleAdd:")
	class, err := objc.RegisterClass(
		"AKSpikeObject",
		Class("NSObject"),
		nil, nil,
		[]objc.MethodDef{{
			Cmd: doubleAdd,
			Fn: func(self ID, _cmd SEL, x int) int {
				observed.called = true
				observed.gotArg = x
				return x*2 + 7
			},
		}},
	)
	if err != nil {
		t.Fatalf("RegisterClass: %v", err)
	}

	inst := ID(class).Send(Sel("alloc")).Send(Sel("init"))
	if inst == nilID {
		t.Fatal("alloc/init returned nil")
	}

	got := objc.Send[int](inst, doubleAdd, 20)
	if !observed.called {
		t.Fatal("Go IMP was never invoked")
	}
	if observed.gotArg != 20 {
		t.Errorf("IMP received arg %d, want 20", observed.gotArg)
	}
	if got != 47 {
		t.Errorf("return value = %d, want 47", got)
	}
}
