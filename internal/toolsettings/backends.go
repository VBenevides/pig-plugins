package toolsettings

// BackendOwner identifies verified wrapper dependencies, not every tool from a search server.
// Apply this relationship only when the wrapper is registered in the current session.
func BackendOwner(name string) string {
	switch name {
	case "mcp__parallel__web_search", "mcp__exa__web_search_exa":
		return "web_search"
	default:
		return ""
	}
}
