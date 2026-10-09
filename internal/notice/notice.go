// Package notice shows extension messages in the chat transcript instead of the transient notification line,
// which renders in the middle of the text input.
package notice

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

const customType = "pig-plugins-notice"

type messenger interface {
	SendMessage(customType, content string, display bool, opts sdk.SendMessageOptions) error
}

type notifier interface {
	Notify(message, level string)
}

// Show displays text in the transcript without starting a model turn and reports whether it did. If the context
// cannot send messages, or sending fails, it falls back to Notify so the message is never lost; callers should
// then also log it. When Show returns true, logging the same text would only add a duplicate line in the terminal.
func Show(ctx notifier, text, level string) bool {
	if m, ok := ctx.(messenger); ok {
		trigger := false
		if err := m.SendMessage(customType, text, true, sdk.SendMessageOptions{TriggerTurn: &trigger}); err == nil {
			return true
		}
	}
	ctx.Notify(text, level)
	return false
}
