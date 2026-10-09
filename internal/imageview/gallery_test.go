package imageview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestGallerySideBySideAndWrapping(t *testing.T) {
	makeImage := func(c color.NRGBA) Image {
		value := image.NewNRGBA(image.Rect(0, 0, 1200, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 1200; x++ {
				value.SetNRGBA(x, y, c)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, value); err != nil {
			t.Fatal(err)
		}
		return Image{Data: base64.StdEncoding.EncodeToString(buf.Bytes()), MimeType: "image/png"}
	}
	entries := []GalleryEntry{{Label: "[Image #1]", Image: makeImage(color.NRGBA{R: 255, A: 255})}, {Label: "[Image #2]", Image: makeImage(color.NRGBA{B: 255, A: 255})}}
	rows, err := Gallery(entries, 80)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if !strings.Contains(rows[0].Label, "[Image #1]") || !strings.Contains(rows[0].Label, "[Image #2]") {
		t.Fatal(rows[0].Label)
	}
	decoded, err := Decode(rows[0].Image)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() < 480 {
		t.Fatal("graphics preview was reduced to terminal cells")
	}
	r, _, _, _ := decoded.At(10, 10).RGBA()
	_, _, b, _ := decoded.At(decoded.Bounds().Dx()/2+30, 10).RGBA()
	if r == 0 || b == 0 {
		t.Fatal("neighbor images are missing or overlap")
	}
	for _, width := range []int{1, 10, 24, 40, 80, 160} {
		rows, err := Gallery(entries, width)
		if err != nil {
			t.Fatal(err)
		}
		if width < 76 && len(rows) != 2 {
			t.Fatalf("width %d did not wrap", width)
		}
		for _, row := range rows {
			if row.Width > width || len(row.Label) > width {
				t.Fatalf("width %d: %+v", width, row)
			}
			if len(row.Fallback) != 10 || strings.Count(row.Fallback[0], "▀") > width {
				t.Fatal("fallback exceeds bounds")
			}
		}
	}
}

func TestGalleryEmptyAndInvalidImage(t *testing.T) {
	if rows, err := Gallery(nil, 80); err != nil || len(rows) != 0 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if _, err := Gallery([]GalleryEntry{{Label: "[Image #3]", Image: Image{Data: "invalid"}}}, 80); err == nil || !strings.Contains(err.Error(), "[Image #3]") {
		t.Fatalf("err=%v", err)
	}
}
