//go:build windows

package utils

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Application icons, read from the executable through shell32/GDI. The PowerShell version of
// this loaded System.Drawing into the shell just to call ExtractAssociatedIcon and re-encode the
// bitmap; the calls below are the same three steps — extract the icon, draw it into a 32-bit
// bitmap, encode a PNG — without a process or a .NET assembly load per listing.

var (
	modshell32 = windows.NewLazySystemDLL("shell32.dll")
	moduser32  = windows.NewLazySystemDLL("user32.dll")
	modgdi32   = windows.NewLazySystemDLL("gdi32.dll")

	procExtractIconEx      = modshell32.NewProc("ExtractIconExW")
	procDestroyIcon        = moduser32.NewProc("DestroyIcon")
	procDrawIconEx         = moduser32.NewProc("DrawIconEx")
	procCreateCompatibleDC = modgdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = modgdi32.NewProc("DeleteDC")
	procCreateDIBSection   = modgdi32.NewProc("CreateDIBSection")
	procSelectObject       = modgdi32.NewProc("SelectObject")
	procDeleteObject       = modgdi32.NewProc("DeleteObject")
)

// iconSize is the natural size of a large shell icon; drawing at native size keeps the bitmap
// sharp, and the UI renders it at 20px.
const iconSize = 32

const diNormal = 0x0003 // DI_NORMAL: draw image and mask

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// iconDataURL returns the executable's icon as a PNG data URL, or "" when it has none.
func iconDataURL(exePath string) string {
	img := exeIconImage(exePath)
	if img == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func exeIconImage(exePath string) *image.NRGBA {
	path, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return nil
	}
	var hicon uintptr
	ret, _, _ := procExtractIconEx.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&hicon)), 0, 1)
	if ret == 0 || hicon == 0 {
		return nil
	}
	defer procDestroyIcon.Call(hicon)
	return drawIconToImage(hicon)
}

// drawIconToImage renders the icon into a top-down 32-bit DIB and copies the pixels out. The
// bitmap is BGRA; a device-independent section is used so the pixels can be read directly
// instead of going back through GetDIBits.
func drawIconToImage(hicon uintptr) *image.NRGBA {
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return nil
	}
	defer procDeleteDC.Call(hdc)

	header := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    iconSize,
		Height:   -iconSize, // negative: top-down rows, the order image.NRGBA wants
		Planes:   1,
		BitCount: 32,
	}
	var bits unsafe.Pointer
	hbitmap, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbitmap == 0 || bits == nil {
		return nil
	}
	defer procDeleteObject.Call(hbitmap)

	previous, _, _ := procSelectObject.Call(hdc, hbitmap)
	ret, _, _ := procDrawIconEx.Call(hdc, 0, 0, hicon, iconSize, iconSize, 0, 0, diNormal)
	procSelectObject.Call(hdc, previous)
	if ret == 0 {
		return nil
	}

	pixels := unsafe.Slice((*byte)(bits), iconSize*iconSize*4)
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	opaque := false
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i] = pixels[i+2] // B G R A -> R G B A
		img.Pix[i+1] = pixels[i+1]
		img.Pix[i+2] = pixels[i]
		img.Pix[i+3] = pixels[i+3]
		if pixels[i+3] != 0 {
			opaque = true
		}
	}
	if !opaque {
		// An icon with only a 1-bit mask leaves the alpha channel untouched, which would encode
		// as a fully transparent PNG. Its mask has already been applied to the colour channels.
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	return img
}
