package toolsettingsext

func coreTool(name string) bool {
	switch name {
	case "read", "write", "edit", "bash", "tool_search":
		return true
	default:
		return false
	}
}
