// png2ico packs one or more PNG files into a Windows .ico (PNG payloads, Vista+).
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

type entry struct {
	size int
	data []byte
}

func main() {
	out := flag.String("o", "", "output .ico path")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: png2ico -o out.ico 16:icon16.png 256:icon256.png")
		os.Exit(2)
	}
	var entries []entry
	for _, arg := range flag.Args() {
		var size int
		var path string
		if _, err := fmt.Sscanf(arg, "%d:%s", &size, &path); err != nil {
			path = arg
			size = 0
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			os.Exit(1)
		}
		if size == 0 {
			size = guessPNGSize(data)
		}
		entries = append(entries, entry{size: size, data: data})
	}
	if err := writeICO(*out, entries); err != nil {
		fmt.Fprintf(os.Stderr, "write ico: %v\n", err)
		os.Exit(1)
	}
}

func guessPNGSize(data []byte) int {
	if len(data) < 24 || data[0] != 0x89 {
		return 256
	}
	return int(binary.BigEndian.Uint32(data[16:20]))
}

func writeICO(path string, entries []entry) error {
	const headerSize = 6
	const dirEntrySize = 16
	n := len(entries)
	buf := make([]byte, headerSize+dirEntrySize*n)
	binary.LittleEndian.PutUint16(buf[0:], 0) // reserved
	binary.LittleEndian.PutUint16(buf[2:], 1) // type ICO
	binary.LittleEndian.PutUint16(buf[4:], uint16(n))
	offset := uint32(headerSize + dirEntrySize*n)
	for i, e := range entries {
		sz := e.size
		if sz >= 256 {
			sz = 0
		}
		base := headerSize + i*dirEntrySize
		buf[base] = byte(sz)
		buf[base+1] = byte(sz)
		buf[base+2] = 0
		buf[base+3] = 0
		binary.LittleEndian.PutUint16(buf[base+4:], 1)  // planes
		binary.LittleEndian.PutUint16(buf[base+6:], 32) // bpp
		binary.LittleEndian.PutUint32(buf[base+8:], uint32(len(e.data)))
		binary.LittleEndian.PutUint32(buf[base+12:], offset)
		offset += uint32(len(e.data))
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := f.Write(e.data); err != nil {
			return err
		}
	}
	return f.Close()
}
