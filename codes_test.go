package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/qr"
)

func withBorder(img image.Image, border int) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()+2*border, b.Dy()+2*border))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(border, border, border+b.Dx(), border+b.Dy()), img, b.Min, draw.Src)
	return out
}

// Labels the app prints must be readable by the app's own photo decoder.
func TestDecodePrintedCodes(t *testing.T) {
	const code = "BIN-00042"
	cases := []struct {
		format string
		make   func() (barcode.Barcode, error)
	}{
		{"qr", func() (barcode.Barcode, error) {
			c, err := qr.Encode(code, qr.M, qr.Auto)
			if err != nil {
				return nil, err
			}
			return barcode.Scale(c, 300, 300)
		}},
		{"code128", func() (barcode.Barcode, error) {
			c, err := code128.Encode(code)
			if err != nil {
				return nil, err
			}
			return barcode.Scale(c, c.Bounds().Dx()*3, 120)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			bc, err := tc.make()
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, withBorder(bc, 40), &jpeg.Options{Quality: 85}); err != nil {
				t.Fatal(err)
			}
			text, format, err := decodeImage(&buf)
			if err != nil || text != code || format != tc.format {
				t.Fatalf("decode = %q, %q, %v; want %q, %q", text, format, err, code, tc.format)
			}
		})
	}
}

func TestDecodeBlankImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeImage(&buf); err != ErrNoCode {
		t.Fatalf("blank image: err = %v, want ErrNoCode", err)
	}
}

func TestRenderSVG(t *testing.T) {
	for _, kind := range []string{"qr", "barcode"} {
		svg, err := renderSVG(kind, "BIN-00001")
		if err != nil || !strings.HasPrefix(string(svg), "<svg") || !strings.Contains(string(svg), `d="M`) {
			t.Fatalf("%s: err = %v, svg = %.80s", kind, err, svg)
		}
	}
	if _, err := renderSVG("nope", "x"); err == nil {
		t.Fatal("expected unknown type to fail")
	}
}
