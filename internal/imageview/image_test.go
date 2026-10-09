package imageview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T, w, h int) Image {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, w, h))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, source); err != nil {
		t.Fatal(err)
	}
	return Image{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}
}
func TestResize(t *testing.T) {
	source := fixture(t, 1600, 800)
	for _, detail := range []bool{false, true} {
		value, err := Resize(source, detail)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(value)
		if err != nil {
			t.Fatal(err)
		}
		want := 480
		if detail {
			want = 1280
		}
		if decoded.Bounds().Dx() != want || decoded.Bounds().Dy() != want/2 {
			t.Fatalf("bounds=%v", decoded.Bounds())
		}
		if value.MimeType != "image/png" {
			t.Fatal(value)
		}
	}
	small := fixture(t, 10, 5)
	value, err := Resize(small, false)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _ := Decode(value)
	if decoded.Bounds().Dx() != 10 {
		t.Fatal("upscaled image")
	}
}
func TestRejectInvalidAndOversized(t *testing.T) {
	for _, value := range []Image{{Data: "!"}, {Data: base64.StdEncoding.EncodeToString([]byte("invalid"))}, {Data: strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxBytes)+1)}} {
		if _, err := Decode(value); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
	if _, err := Read(t.TempDir()); err == nil {
		t.Fatal("accepted directory")
	}
}
func TestBlobConcurrentIdempotency(t *testing.T) {
	root := t.TempDir()
	value := fixture(t, 4, 4)
	var wg sync.WaitGroup
	refs := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			ref, err := Store(value, root)
			if err != nil {
				t.Error(err)
				return
			}
			refs <- ref
		})
	}
	wg.Wait()
	close(refs)
	want := ""
	for ref := range refs {
		if want == "" {
			want = ref
		}
		if ref != want {
			t.Fatal("different hashes")
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v error=%v", files, err)
	}
	data, err := os.ReadFile(filepath.Join(root, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(data) != value.Data {
		t.Fatal("incomplete blob")
	}
	if Strip("[[Image #7]]("+want+")") != "[Image #7]" {
		t.Fatal("link stripping")
	}
}
func TestPathsAndThumbnail(t *testing.T) {
	paths := Paths(`look "./my photo.png" and ./test\ image.jpg`, "/work")
	if len(paths) != 2 || paths[0].File != "/work/my photo.png" || paths[1].File != "/work/test image.jpg" {
		t.Fatal(paths)
	}
	lines, err := Thumbnail(fixture(t, 20, 10))
	if err != nil || len(lines) != 6 {
		t.Fatalf("lines=%d err=%v", len(lines), err)
	}
}
