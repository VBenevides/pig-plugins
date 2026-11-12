package websearchext

import sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

// Backend execution stays available through ExecuteTool and its normal permission hooks.
// Only declarations are hidden; no global permission or transport state changes here.
func (*extension) prepareLoadout(_ sdk.ToolLoadout) *sdk.ToolLoadoutChanges {
	return &sdk.ToolLoadoutChanges{HiddenDeclarations: []string{
		"mcp__parallel__web_search", "mcp__exa__web_search_exa",
	}}
}
