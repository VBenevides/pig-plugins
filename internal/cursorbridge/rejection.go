package cursorbridge

// Exact text emitted by the pinned bridge's sandbox preflight. It is matched,
// never echoed: SDK advice to disable sandboxing is not safe for this provider.
const sandboxUnavailable = "Local SDK sandboxing was requested, but sandboxing is not supported in this environment. Disable local.sandboxOptions.enabled or remove ~/.cursor/sandbox.json to run without sandboxing."

func rejectionReason(reason string) string {
	switch reason {
	case "sandbox_unavailable":
		return "The Cursor SDK sandbox is unavailable. The bridge requires its cursorsandbox helper and a successful sandbox preflight. PiG keeps sandboxing enabled; no unsandboxed fallback was attempted"
	case "invalid_model":
		return "The SDK rejected the selected model. Refresh the Cursor catalog and select a listed model and thinking level; PiG will not substitute another model"
	case "validation_error":
		return "The SDK rejected the request configuration. Model and tool schema compatibility still need investigation"
	case "plan_required":
		return "Cursor requires a plan for this operation. Check the account's plan and team policy"
	default:
		return ""
	}
}
