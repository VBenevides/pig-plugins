package requestobservation

import (
	"encoding/json"
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/compactlimit"
)

func Extension() *sdk.Extension {
	e := sdk.New("request-observation")
	e.OnEvent(sdk.EventBeforeProviderRequest, func(_ sdk.Context, data map[string]any) (any, error) {
		payload, ok := data["payload"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("probe payload is %T", data["payload"])
		}
		payload["temperature"] = 0.25
		return payload, nil
	})
	e.OnEvent(compactlimit.RequestObserved, func(ctx sdk.Context, data map[string]any) (any, error) {
		measurement, err := compactlimit.ObserveRequest(ctx, data)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(map[string]any{"measurement": measurement, "payload": data["payload"]})
		if err != nil {
			return nil, err
		}
		ctx.Notify("request-observation:"+string(encoded), "info")
		// Neither mutation nor the return value may affect another observer or the provider.
		data["payload"].(map[string]any)["temperature"] = 99
		return map[string]any{"invalid": true}, nil
	})
	e.OnEvent(compactlimit.RequestObserved, func(ctx sdk.Context, data map[string]any) (any, error) {
		if data["payload"].(map[string]any)["temperature"] != 0.25 {
			return nil, fmt.Errorf("observer copy leaked")
		}
		ctx.Notify("request-observer-isolated", "info")
		return nil, nil
	})
	return e
}
