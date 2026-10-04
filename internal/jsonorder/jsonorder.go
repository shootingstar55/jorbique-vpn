// Package jsonorder reads and writes JSON without rearranging it.
//
// The sing-box profile is written by hand on the server, and the client rewrites
// parts of it — credentials, paths, the outbounds a user turned off. Decoding it
// into a Go map loses the order the author wrote, so the file that came back
// listed the same fields in a different sequence every time, and a diff between
// "what the server serves" and "what the client runs" was unreadable.
//
// Encoding here also leaves strings alone: the standard encoder escapes <, > and
// & into < and friends, which is valid JSON and unreadable to the person
// checking a pre-shared key by eye.
package jsonorder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Map is a JSON object that remembers the order its keys arrived in.
type Map struct {
	keys   []string
	values map[string]any
}

// New returns an empty object.
func New() *Map { return &Map{values: map[string]any{}} }

// Keys returns the keys in order.
func (m *Map) Keys() []string {
	out := make([]string, len(m.keys))
	copy(out, m.keys)
	return out
}

// Has reports whether the key is present, whatever its value.
func (m *Map) Has(k string) bool { _, ok := m.values[k]; return ok }

// Get returns the value and whether the key is present.
func (m *Map) Get(k string) (any, bool) {
	v, ok := m.values[k]
	return v, ok
}

// GetString returns a string value, empty when absent or of another type.
func (m *Map) GetString(k string) string {
	s, _ := m.values[k].(string)
	return s
}

// Set stores a value, appending the key when it is new and keeping its place
// when it is not: rewriting a field must not move it.
func (m *Map) Set(k string, v any) {
	if _, ok := m.values[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.values[k] = v
}

// SortKeys reorders the keys to follow order. A key order does not mention
// keeps its place relative to the other unmentioned ones and lands after the
// mentioned ones — so a section this client has never heard of survives the
// sort instead of being dropped or shuffled.
func (m *Map) SortKeys(order []string) {
	rank := make(map[string]int, len(order))
	for i, k := range order {
		rank[k] = i
	}
	sort.SliceStable(m.keys, func(i, j int) bool {
		ri, iKnown := rank[m.keys[i]]
		rj, jKnown := rank[m.keys[j]]
		if iKnown && jKnown {
			return ri < rj
		}
		return iKnown && !jKnown
	})
}

// Delete removes a key.
func (m *Map) Delete(k string) {
	if _, ok := m.values[k]; !ok {
		return
	}
	delete(m.values, k)
	for i, kk := range m.keys {
		if kk == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

// Len is the number of keys.
func (m *Map) Len() int { return len(m.keys) }

// UnmarshalJSON decodes an object, keeping the order of its keys and decoding
// numbers as json.Number so an integer port does not come back as 49211.0.
func (m *Map) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	m.keys = nil
	m.values = map[string]any{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := kt.(string)
		if !ok {
			return errors.New("expected string key")
		}
		v, err := decodeValue(dec)
		if err != nil {
			return err
		}
		m.Set(key, v)
	}
	_, err = dec.Token()
	return err
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	v, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch v {
	case '{':
		return decodeObject(dec)
	case '[':
		return decodeArray(dec)
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", v)
	}
}

func decodeObject(dec *json.Decoder) (*Map, error) {
	obj := New()
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, errors.New("expected string key")
		}
		value, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		obj.Set(key, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return obj, nil
}

func decodeArray(dec *json.Decoder) ([]any, error) {
	arr := []any{}
	for dec.More() {
		value, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return arr, nil
}

// Unmarshal decodes a whole document into an ordered object.
func Unmarshal(data []byte, m *Map) error { return m.UnmarshalJSON(data) }

// MarshalJSON writes the object in key order.
func (m *Map) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := encodeScalar(k)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		val, err := marshalValue(m.values[k])
		if err != nil {
			return nil, err
		}
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func marshalValue(v any) ([]byte, error) {
	switch val := v.(type) {
	case *Map:
		return val.MarshalJSON()
	case []any:
		var b bytes.Buffer
		b.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				b.WriteByte(',')
			}
			enc, err := marshalValue(item)
			if err != nil {
				return nil, err
			}
			b.Write(enc)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	default:
		return encodeScalar(v)
	}
}

// encodeScalar writes one value the way it was written in the profile: a
// pre-shared key full of <, > and & stays readable instead of turning into
// <, and a json.Number keeps the digits it arrived with.
func encodeScalar(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// MarshalIndent renders the document with two-space indentation, in the order
// the keys are held.
func MarshalIndent(v any) ([]byte, error) {
	compact, err := marshalValue(v)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", "  "); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// FromMap builds an ordered object from a plain map, keys sorted. Meant for
// tests and for values the client composes itself, where there is no author's
// order to preserve — a sorted one at least makes the output reproducible.
func FromMap(m map[string]any) *Map {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := New()
	for _, k := range keys {
		out.Set(k, convert(m[k]))
	}
	return out
}

func convert(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return FromMap(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = convert(item)
		}
		return out
	default:
		return val
	}
}
