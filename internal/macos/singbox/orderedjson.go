package singbox

import "jorbiquevpn/internal/jsonorder"

// OrderedMap is the shared ordered JSON object. It used to be implemented here,
// alongside a pretty-printer that imposed its own key order on every object it
// wrote; both clients now read and write a profile in the order its author wrote
// it, from one place.
type OrderedMap = jsonorder.Map

// NewOrderedMap returns an empty ordered object.
func NewOrderedMap() *OrderedMap { return jsonorder.New() }
