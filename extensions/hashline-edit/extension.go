// Package hashlineedit replaces the built-in read, edit, and write tools.
// Anchored edits and explicit writes share internal/hashline's mutation coordinator.
package hashlineedit

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/hashline"
)

// Extension replaces the built-in read, edit, and write tool registrations.
func Extension() *sdk.Extension {
	e := sdk.New("hashline-edit")
	engine := hashline.NewEngine()
	for _, event := range []string{"session_start", "session_tree", "session_shutdown"} {
		e.OnEvent(event, func(_ sdk.Context, _ map[string]any) (any, error) {
			engine.Invalidate()
			return nil, nil
		})
	}
	e.RegisterTool(sdk.ToolDefinition{
		Name:             "read",
		Label:            "read",
		Description:      hashline.ReadDescription,
		PromptSnippet:    hashline.ReadSnippet,
		PromptGuidelines: hashline.ReadGuidelines,
		Parameters:       hashline.ReadSchema(),
		Execute:          run(engine.ReadContext),
	})
	e.RegisterTool(sdk.ToolDefinition{
		Name:             "edit",
		Label:            "edit",
		Description:      hashline.EditDescription,
		PromptSnippet:    hashline.EditSnippet,
		PromptGuidelines: hashline.EditGuidelines,
		Parameters:       hashline.EditSchema(),
		Execute:          run(engine.EditContext),
	})
	e.RegisterTool(sdk.ToolDefinition{
		Name: "write", Label: "write", Description: hashline.WriteDescription,
		Parameters: hashline.WriteSchema(), Execute: run(engine.WriteContext),
	})
	return e
}

func run(tool func(request hashline.Cancellation, cwd string, params map[string]any) (hashline.Result, error)) sdk.ToolFunc {
	return func(ctx sdk.Context, params map[string]any) (any, error) {
		session, err := ctx.GetSessionID()
		if err != nil {
			return nil, err
		}
		result, err := tool(hashline.WithScope(ctx, session), ctx.Cwd(), params)
		if err != nil {
			return nil, err
		}
		out := sdk.ToolResult{Content: result.Text, Details: result.Details}
		for _, image := range result.Images {
			out.Images = append(out.Images, sdk.ImageContent{Data: image.Data, MimeType: image.MimeType})
		}
		return out, nil
	}
}
