package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func fakePNG(width uint32) []byte {
	b := make([]byte, 24)
	b[0] = 0x89
	copy(b[1:8], []byte("PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(b[16:20], width)
	return b
}

func TestGuessPNGSize(t *testing.T) {
	if got := guessPNGSize(fakePNG(64)); got != 64 {
		t.Errorf("guessPNGSize = %d, want 64", got)
	}
	// Too short / not a PNG -> default 256.
	if got := guessPNGSize([]byte{0x00, 0x01}); got != 256 {
		t.Errorf("short data => %d, want 256", got)
	}
	if got := guessPNGSize(make([]byte, 24)); got != 256 {
		t.Errorf("non-PNG signature => %d, want 256", got)
	}
}

func TestWriteICO(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "icon.ico")
	entries := []entry{
		{size: 16, data: fakePNG(16)},
		{size: 256, data: fakePNG(256)},
	}
	if err := writeICO(out, entries); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// ICONDIR header: reserved=0, type=1, count=2.
	if binary.LittleEndian.Uint16(raw[0:]) != 0 {
		t.Error("reserved field should be 0")
	}
	if binary.LittleEndian.Uint16(raw[2:]) != 1 {
		t.Error("type should be 1 (icon)")
	}
	if n := binary.LittleEndian.Uint16(raw[4:]); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}

	// First dir entry: width byte 16, data length matches.
	const headerSize, dirEntrySize = 6, 16
	if raw[headerSize] != 16 {
		t.Errorf("entry[0] width byte = %d, want 16", raw[headerSize])
	}
	if l := binary.LittleEndian.Uint32(raw[headerSize+8:]); l != uint32(len(entries[0].data)) {
		t.Errorf("entry[0] data length = %d", l)
	}
	// Second entry: 256 is encoded as width byte 0.
	base := headerSize + dirEntrySize
	if raw[base] != 0 {
		t.Errorf("entry[1] width byte = %d, want 0 (means 256)", raw[base])
	}

	// Total file size = header + dir + all payloads.
	want := headerSize + dirEntrySize*2 + len(entries[0].data) + len(entries[1].data)
	if len(raw) != want {
		t.Errorf("file size = %d, want %d", len(raw), want)
	}
}
