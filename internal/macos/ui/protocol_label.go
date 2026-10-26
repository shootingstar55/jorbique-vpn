package ui

// protocolLabels are the names the window's protocol list shows
// (frontend/src/lib/outbounds.js), so the menu bar names a protocol the same
// way. A tag missing here is shown as the server named it.
var protocolLabels = map[string]string{
	"vless-reality":      "VLESS Reality",
	"vless-grpc":         "VLESS gRPC",
	"vless-reality-rmux": "VLESS Reality · RMUX",
	"vless-xhttp":        "VLESS XHTTP",
	"mieru":              "Mieru",
	"sudoku":             "Sudoku",
	"anytls":             "AnyTLS",
	"snell":              "Snell",
	"naive":              "NaiveProxy",
}

func protocolLabel(tag string) string {
	if label, ok := protocolLabels[tag]; ok {
		return label
	}
	return tag
}
