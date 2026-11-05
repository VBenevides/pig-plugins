package distribution

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/VBenevides/pig-plugins/internal/agentdir"
	"github.com/VBenevides/pig-plugins/internal/cursorbridge"
)

// InstallCursor handles the explicit standalone flag before provider startup.
// Normal startup never calls the installer.
func InstallCursor(args []string) (bool, error) {
	requested, err := cursorbridge.InstallFlag(args)
	if err != nil || !requested {
		return requested, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, err = cursorbridge.Install(ctx, agentdir.Dir(os.Getenv), runtime.GOOS, runtime.GOARCH)
	return true, err
}
