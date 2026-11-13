package toolsettings

import (
	"strings"
	"testing"
)

func TestDefaultProfileHelpDescribesNativeDiscovery(t *testing.T) {
	menu := &Menu{PresetPending: true}
	rendered := strings.Join(menu.Render(100), "\n")
	if !strings.Contains(rendered, "1 default  · native core + discovery; all skills") {
		t.Fatal("default profile help does not describe native tools and discovery")
	}
	if strings.Contains(rendered, "1 default  · default tools + all skills; Hashline") {
		t.Fatal("obsolete default profile help remains")
	}
}
