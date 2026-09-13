package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/qr"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

var ErrNoCode = errors.New("no code found in image")

// renderSVG draws a QR code or Code 128 barcode as SVG so labels print sharp
// at any size on any printer.
func renderSVG(kind, data string) ([]byte, error) {
	if data == "" || len(data) > 500 {
		return nil, invalid("Nothing to encode.")
	}
	switch kind {
	case "qr":
		bc, err := qr.Encode(data, qr.M, qr.Auto)
		if err != nil {
			return nil, invalid("Can't make a QR code: %v", err)
		}
		return qrSVG(bc), nil
	case "barcode":
		bc, err := code128.Encode(data)
		if err != nil {
			return nil, invalid("Can't make a barcode: %v", err)
		}
		return barcodeSVG(bc), nil
	}
	return nil, invalid("Unknown code type %q.", kind)
}

func isDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r+g+b < 3*0x8000
}

func qrSVG(bc barcode.Barcode) []byte {
	const quiet = 2
	bounds := bc.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	var path strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			if !isDark(bc.At(bounds.Min.X+x, bounds.Min.Y+y)) {
				x++
				continue
			}
			start := x
			for x < w && isDark(bc.At(bounds.Min.X+x, bounds.Min.Y+y)) {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start+quiet, y+quiet, x-start, x-start)
		}
	}
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		w+2*quiet, h+2*quiet, path.String()))
}

func barcodeSVG(bc barcode.Barcode) []byte {
	const quiet = 10
	const height = 40
	bounds := bc.Bounds()
	w := bounds.Dx()
	var path strings.Builder
	for x := 0; x < w; {
		if !isDark(bc.At(bounds.Min.X+x, bounds.Min.Y)) {
			x++
			continue
		}
		start := x
		for x < w && isDark(bc.At(bounds.Min.X+x, bounds.Min.Y)) {
			x++
		}
		fmt.Fprintf(&path, "M%d 0h%dv%dh-%dz", start+quiet, x-start, height, x-start)
	}
	// preserveAspectRatio="none" lets the bars stretch evenly to the label width.
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" preserveAspectRatio="none" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		w+2*quiet, height, path.String()))
}

// decodeImage finds a QR code or common 1D barcode in a photo or camera frame.
func decodeImage(r io.Reader) (text, format string, err error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return "", "", invalid("Couldn't read that image.")
	}
	defer func() {
		// The decoder can panic on unusual images; treat that as "not found".
		if recover() != nil {
			text, format, err = "", "", ErrNoCode
		}
	}()

	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", "", ErrNoCode
	}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	readers := []struct {
		name   string
		reader gozxing.Reader
	}{
		{"qr", qrcode.NewQRCodeReader()},
		{"code128", oned.NewCode128Reader()},
		{"code39", oned.NewCode39Reader()},
		{"ean/upc", oned.NewMultiFormatUPCEANReader(hints)},
	}
	for _, rd := range readers {
		if res, err := rd.reader.Decode(bmp, hints); err == nil && res.GetText() != "" {
			return res.GetText(), rd.name, nil
		}
	}
	return "", "", ErrNoCode
}
