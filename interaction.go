package pigplugins

import (
	"fmt"

	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

// interactionStatus updates the fused footer mirror without adding a host badge.
type interactionStatus struct{}

func (interactionStatus) SetStatus(string, string) {}

// SetInteractionPage publishes the display-only page selected by the host.
// Session records and model history are not changed.
func SetInteractionPage(current, total int) {
	text := ""
	if total > 0 && current > 0 && current <= total {
		text = fmt.Sprintf("%d/%d", current, total)
	}
	if footerstatus.Snapshot()["interaction-page"] != text {
		footerstatus.Set(interactionStatus{}, "interaction-page", text)
	}
}
