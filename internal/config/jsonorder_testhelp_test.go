package config

import "jorbiquevpn/internal/jsonorder"

// mustField reads a typed field out of an ordered object, the way a plain map
// index used to read one. Tests here poke at configs constantly; without this
// every read would be three lines of type assertion.
func mustField[T any](m *jsonorder.Map, key string) (T, bool) {
	var zero T
	if m == nil {
		return zero, false
	}
	v, ok := m.Get(key)
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// field is mustField for the callers that do not care whether it was there.
func field[T any](m *jsonorder.Map, key string) T {
	v, _ := mustField[T](m, key)
	return v
}
