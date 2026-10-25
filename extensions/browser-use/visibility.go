package browseruseext

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/browseruse"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

func (s *state) setVisibility(ctx sdk.Context, visibility browseruse.Visibility) error {
	if visibility != browseruse.Hidden && visibility != browseruse.Login && visibility != browseruse.Always {
		return fmt.Errorf("invalid browser visibility %q", visibility)
	}
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.changeMu.Lock()
	defer s.changeMu.Unlock()
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client != nil {
		request, cancel := sdkctx.Request(ctx)
		defer cancel()
		bounded, stop := context.WithTimeout(request, 90*time.Second)
		defer stop()
		if err := client.SetVisible(bounded, visibility == browseruse.Always); err != nil {
			return fmt.Errorf("change browser visibility: %w", err)
		}
	}
	s.mu.Lock()
	s.visibility = visibility
	s.mu.Unlock()
	return nil
}
