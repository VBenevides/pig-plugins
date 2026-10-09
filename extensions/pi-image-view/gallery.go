package imageviewext

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

type previewGraphics interface {
	RenderImage(data string, width, height int) ([]string, bool, error)
	SetPreviewWidget(key string, lines []string, width int) error
}

func showGallery(ctx sdk.Context, entries []draftImage, width int) error {
	if !ctx.HasUI() || ctx.Mode() != "tui" {
		return nil
	}
	if len(entries) == 0 {
		return ctx.SetWidget("image-view", nil)
	}
	graphics, ok := any(ctx).(previewGraphics)
	if !ok {
		return fmt.Errorf("image gallery requires the patched SDK; rebuild with scripts/dev_build.sh")
	}
	width = max(1, width)
	values := make([]images.GalleryEntry, len(entries))
	for index, entry := range entries {
		values[index] = images.GalleryEntry{Label: entry.Label, Image: entry.Preview}
	}
	rows, err := images.Gallery(values, width)
	if err != nil {
		return err
	}
	var lines []string
	for _, row := range rows {
		frame, supported, err := graphics.RenderImage(row.Image.Data, row.Width, 10)
		if err != nil {
			return fmt.Errorf("render preview %s: %w", row.Label, err)
		}
		if !supported {
			frame = row.Fallback
		}
		lines = append(lines, row.Label)
		lines = append(lines, frame...)
	}
	if err := graphics.SetPreviewWidget("image-view", lines, width); err != nil {
		return fmt.Errorf("display image gallery: %w", err)
	}
	return nil
}
