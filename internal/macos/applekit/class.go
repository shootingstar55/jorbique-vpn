//go:build darwin

package applekit

import "github.com/ebitengine/purego/objc"

// method describes one Go-implemented Objective-C method: the selector it
// answers to and the Go function backing it. The function's first two
// parameters are always (self ID, _cmd SEL); remaining parameters and the
// return value are marshaled by purego according to their Go types, and the
// Objective-C type encoding is derived from the signature automatically.
type method struct {
	cmd SEL
	fn  any
}

// registerClass creates a new Objective-C class at runtime as a subclass of the
// named superclass, installing the given Go-backed methods. This is the single
// mechanism behind every custom subclass this codebase needs (hover buttons,
// window/panel delegates, custom text fields). Keeping it in one place means the
// objc_allocateClassPair / class_addMethod machinery — proven in the Phase 0
// gate — is reviewed once, not re-derived per panel.
//
// Registering the same class name twice in a process panics inside the Objective-C
// runtime, so callers must guard registration to run once (see the *Registered
// sentinels used by the concrete subclasses).
func registerClass(name, superName string, methods []method) (objc.Class, error) {
	defs := make([]objc.MethodDef, len(methods))
	for i, m := range methods {
		defs[i] = objc.MethodDef{Cmd: m.cmd, Fn: m.fn}
	}
	return objc.RegisterClass(name, Class(superName), nil, nil, defs)
}
