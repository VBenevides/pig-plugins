package checkupdate

import (
	"context"
	"fmt"
	"net/http"

	pigplugins "github.com/VBenevides/pig-plugins"
	"github.com/VBenevides/pig-plugins/internal/updates"
)

// checkUpdates isolates source failures and only reports upgrades; it never installs them.
func checkUpdates(ctx context.Context, notify func(string, string), client *http.Client, pigVersion, compatibilityURL, pluginsURL string) {
	for _, source := range []struct {
		name, url, current string
		compatible         bool
	}{
		{"PiG", compatibilityURL, pigVersion, true},
		{"PiG Plugins", pluginsURL, pigplugins.Version(), false},
	} {
		var latest string
		var err error
		if source.compatible {
			latest, err = updates.LatestCompatiblePiG(ctx, client, source.url)
		} else {
			latest, err = updates.Latest(ctx, client, source.url, false)
		}
		if err != nil {
			notify(source.name+" update check failed: "+err.Error(), "warning")
			continue
		}
		newer, err := updates.Newer(latest, source.current)
		if err != nil {
			notify(source.name+" update comparison failed: "+err.Error(), "warning")
			continue
		}
		if !newer {
			continue
		}
		message := fmt.Sprintf("%s %s is available. Run pig-plugins --update to reinstall the latest compatible distribution.", source.name, latest)
		notify(message, "info")
	}
}
