package imageview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
)

// GalleryEntry is one numbered draft preview, independent of model attachments.
type GalleryEntry struct {
	Label string
	Image Image
}

type GalleryRow struct {
	Label    string
	Image    Image
	Width    int
	Fallback []string
}

// Gallery groups previews into width-bounded rows. Each graphics row is a single
// PNG, so adjacent images do not depend on cursor movement or overlapping placements.
func Gallery(entries []GalleryEntry, width int) ([]GalleryRow, error) {
	if width <= 0 {
		width = 80
	}
	width = min(width, 240)
	columns := max(1, min(4, (width+2)/38))
	tileWidth := min(48, max(1, (width-2*(columns-1))/columns))
	var rows []GalleryRow
	for start := 0; start < len(entries); start += columns {
		batch := entries[start:min(start+columns, len(entries))]
		rowWidth := len(batch)*tileWidth + (len(batch)-1)*2
		canvas := image.NewNRGBA(image.Rect(0, 0, rowWidth*12, 240))
		var labels strings.Builder
		for index, entry := range batch {
			source, err := Decode(entry.Image)
			if err != nil {
				return nil, fmt.Errorf("gallery %s: %w", entry.Label, err)
			}
			label := entry.Label
			if len(label) > tileWidth {
				label = label[:tileWidth]
			}
			labels.WriteString(label)
			if index < len(batch)-1 {
				labels.WriteString(strings.Repeat(" ", tileWidth-len(label)+2))
			}
			bounds := source.Bounds()
			w, h := bounds.Dx(), bounds.Dy()
			if w*240 > h*tileWidth*12 {
				h = max(1, h*tileWidth*12/w)
				w = tileWidth * 12
			} else {
				w = max(1, w*240/h)
				h = 240
			}
			x := index * (tileWidth + 2) * 12
			target := image.Rect(x, 0, x+w, h)
			draw.CatmullRom.Scale(canvas, target, source, bounds, draw.Src, nil)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, canvas); err != nil {
			return nil, fmt.Errorf("encode gallery: %w", err)
		}
		value := Image{Type: "image", Data: base64.StdEncoding.EncodeToString(buf.Bytes()), MimeType: "image/png"}
		fallback := thumbnail(canvas, rowWidth, 20)
		rows = append(rows, GalleryRow{Label: labels.String(), Image: value, Width: rowWidth, Fallback: fallback})
	}
	return rows, nil
}

func thumbnail(source image.Image, width, height int) []string {
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(target, target.Bounds(), source, source.Bounds(), draw.Src, nil)
	var lines []string
	for y := 0; y < height; y += 2 {
		var line strings.Builder
		for x := 0; x < width; x++ {
			a, b := target.NRGBAAt(x, y), target.NRGBAAt(x, y+1)
			fmt.Fprintf(&line, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", a.R, a.G, a.B, b.R, b.G, b.B)
		}
		lines = append(lines, line.String()+"\x1b[0m")
	}
	return lines
}
