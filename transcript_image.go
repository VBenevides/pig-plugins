package pigplugins

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

// ReadStoredImage reads a bounded image-view blob for transcript display only.
// It never fetches remote URLs or changes the model's message content.
func ReadStoredImage(reference string) (data, mimeType string, err error) {
	match := images.Reference.FindStringSubmatch(reference)
	if len(match) != 2 || match[0] != reference {
		return "", "", fmt.Errorf("invalid stored image reference %q", reference)
	}
	path := filepath.Join(agentdir.Dir(os.Getenv), "image-view", "blobs", match[1])
	value, err := images.Read(path)
	if err != nil {
		return "", "", fmt.Errorf("read stored image %s: %w", match[1], err)
	}
	return value.Data, mime.TypeByExtension(filepath.Ext(path)), nil
}
