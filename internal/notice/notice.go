// Package notice routes display-only extension diagnostics to the host UI.
// Never use conversation messages for notifications: native custom messages are
// converted to user messages in the next model request, even with TriggerTurn=false.
package notice

type notifier interface {
	Notify(message, level string)
}

// Show dispatches a diagnostic through the host's notification channel, which
// the pinned native host renders in the transcript without touching the editor
// or model history. The result reports UI availability, not an acknowledged
// render (the SDK Notify API returns no error). Callers should log when false.
func Show(ctx notifier, text, level string) bool {
	if ui, ok := ctx.(interface{ HasUI() bool }); ok && !ui.HasUI() {
		return false
	}
	ctx.Notify(text, level)
	return true
}
