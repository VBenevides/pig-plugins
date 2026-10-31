package compactlimit

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

// ObserveRequest is shared by both context extensions. The host supplies a private
// snapshot after all payload transforms. It is not a safe place to await Compact:
// a manual compact aborts and joins the agent goroutine that awaits this event.
func ObserveRequest(ctx sdk.Context, data map[string]any) (RequestMeasurement, error) {
	session, err := ctx.SessionManager().GetSessionID()
	if err != nil {
		return RequestMeasurement{}, fmt.Errorf("observe request session: %w", err)
	}
	leaf, err := ctx.SessionManager().GetLeafID()
	if err != nil {
		return RequestMeasurement{}, fmt.Errorf("observe request branch: %w", err)
	}
	branch := ""
	if leaf != nil {
		branch = *leaf
	}
	identity := session + "\x00" + branch + "\x00" + ctx.ModelProvider() + "\x00" + ctx.Model()
	return MeasureRequest(data["payload"], identity, outputReservation(data["payload"]))
}

func outputReservation(payload any) int {
	object, _ := payload.(map[string]any)
	for _, key := range []string{"max_completion_tokens", "max_output_tokens", "max_tokens"} {
		if number, ok := object[key].(float64); ok && number > 0 && number < float64(1<<31) {
			return int(number)
		}
	}
	return 0 // Unknown or adapter-defaulted output reservation, not an input-token deduction.
}
