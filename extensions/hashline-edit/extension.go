// Package hashlineedit is the PiG extension "hashline-edit": it replaces the built-in read and edit tools with
// anchored read and strict anchored edit. The behavior lives in internal/hashline.
package hashlineedit

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/hashline"
)

// Extension registers read and edit. Registering a built-in tool name replaces the built-in.
func Extension() *sdk.Extension {
	e := sdk.New("hashline-edit")
	e.RegisterTool(sdk.ToolDefinition{
		Name:             "read",
		Label:            "read",
		Description:      hashline.ReadDescription,
		PromptSnippet:    hashline.ReadSnippet,
		PromptGuidelines: hashline.ReadGuidelines,
		Parameters:       hashline.ReadSchema(),
		Execute:          run(hashline.Read),
	})
	e.RegisterTool(sdk.ToolDefinition{
		Name:             "edit",
		Label:            "edit",
		Description:      hashline.EditDescription,
		PromptSnippet:    hashline.EditSnippet,
		PromptGuidelines: hashline.EditGuidelines,
		Parameters:       hashline.EditSchema(),
		Execute:          run(hashline.Edit),
	})
	return e
}

func run(tool func(cwd string, params map[string]any) (hashline.Result, error)) sdk.ToolFunc {
	return func(ctx sdk.Context, params map[string]any) (any, error) {
		result, err := tool(ctx.Cwd(), params)
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
