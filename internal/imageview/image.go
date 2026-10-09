// Package imageview ports pi-image-view's attachment processing to native Go.
package imageview

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxBytes = 32 << 20
const MaxPixels = 24_000_000

type Image struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func Decode(value Image) (image.Image, error) {
	if len(value.Data) > base64.StdEncoding.EncodedLen(MaxBytes) {
		return nil, fmt.Errorf("image exceeds %d bytes", MaxBytes)
	}
	data, err := base64.StdEncoding.DecodeString(value.Data)
	if err != nil {
		return nil, fmt.Errorf("decode image base64: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read image dimensions: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxPixels/cfg.Height {
		return nil, fmt.Errorf("image exceeds %d pixels", MaxPixels)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return decoded, nil
}

func Resize(value Image, detail bool) (Image, error) {
	source, err := Decode(value)
	if err != nil {
		return Image{}, err
	}
	limit, budget := 480, 2<<20
	if detail {
		limit, budget = 1280, 4500<<10
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > limit || h > limit {
		if w >= h {
			h = max(1, h*limit/w)
			w = limit
		} else {
			w = max(1, w*limit/h)
			h = limit
		}
	}
	for {
		target := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, target); err != nil {
			return Image{}, fmt.Errorf("encode preview: %w", err)
		}
		if buf.Len() <= budget {
			return Image{Type: "image", Data: base64.StdEncoding.EncodeToString(buf.Bytes()), MimeType: "image/png"}, nil
		}
		if w == 1 && h == 1 {
			return Image{}, fmt.Errorf("preview exceeds byte budget")
		}
		w, h = max(1, w/2), max(1, h/2)
	}
}

func Read(path string) (Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return Image{}, fmt.Errorf("open image %q: %w", path, err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return Image{}, err
	}
	if !stat.Mode().IsRegular() {
		return Image{}, fmt.Errorf("image %q is not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return Image{}, fmt.Errorf("read image %q: %w", path, err)
	}
	if len(data) > MaxBytes {
		return Image{}, fmt.Errorf("image %q exceeds byte limit", path)
	}
	return Image{Type: "image", Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func Store(value Image, root string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(value.Data)
	if err != nil {
		return "", err
	}
	if len(data) > MaxBytes {
		return "", fmt.Errorf("blob exceeds byte limit")
	}
	if value.MimeType != "image/png" {
		return "", fmt.Errorf("only prepared PNG blobs can be stored")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", fmt.Errorf("create blob directory: %w", err)
	}
	name := fmt.Sprintf("%x.png", sha256.Sum256(data))
	destination := filepath.Join(root, name)
	// Publish only a fully written blob. Hard linking is exclusive and leaves existing blobs unchanged.
	f, err := os.CreateTemp(root, ".image-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("write image blob: %w", err)
	}
	if err = os.Link(f.Name(), destination); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("publish image blob: %w", err)
	}
	return "image-view://sha256/" + name, nil
}

var Reference = regexp.MustCompile(`image-view://sha256/([a-f0-9]{64}\.(?:png|jpg|gif|webp))`)
var Links = regexp.MustCompile(`\[?\[Image #(\d+)\]\]?\((image-view://sha256/[a-f0-9]{64}\.(?:png|jpg|gif|webp)|file:///[^)\n]*/image-view/blobs/[a-f0-9]{64}\.(?:png|jpg|gif|webp))\)`)
var Markers = regexp.MustCompile(`\[Image #(\d+)\]`)

func Strip(text string) string { return Links.ReplaceAllString(text, "[Image #$1]") }

type Path struct{ Raw, File string }

var paths = regexp.MustCompile(`(?i)"(?:\\.|[^"\\])*\.(?:png|jpe?g|gif|webp)"|'(?:\\.|[^'\\])*\.(?:png|jpe?g|gif|webp)'|(?:~/|\.\.?/|/)(?:\\.|[^\s:*?"<>|])*\.(?:png|jpe?g|gif|webp)`)
var escapes = regexp.MustCompile(`\\(.)`)

func Paths(text, cwd string) []Path {
	var result []Path
	for _, raw := range paths.FindAllString(text, -1) {
		value := strings.Trim(raw, "\"'")
		value = escapes.ReplaceAllString(value, "$1")
		if strings.HasPrefix(value, "~/") {
			home, _ := os.UserHomeDir()
			value = filepath.Join(home, value[2:])
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(cwd, value)
		}
		result = append(result, Path{raw, value})
	}
	return result
}

// Thumbnail uses terminal cells rather than owning Kitty terminal resources.
func Thumbnail(value Image) ([]string, error) {
	source, err := Decode(value)
	if err != nil {
		return nil, err
	}
	return thumbnail(source, 24, 12), nil
}
