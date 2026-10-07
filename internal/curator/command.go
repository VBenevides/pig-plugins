package curator

import (
	"context"
	"fmt"
	"strings"
)

// CommandName is the slash command.
const CommandName = "pi-curator"

// CommandHelp is shown for a malformed command.
const CommandHelp = "usage: /pi-curator [status | off | on | prefetch <off|64..8192> | startup <off|1..10> | engine <legacy|fts|hybrid|episodes|state>]"

// Completion is one argument completion.
type Completion struct{ Value, Label string }

// Completions lists the completions for the argument text typed so far.
func Completions(prefix string) []Completion {
	words := strings.Fields(prefix)
	if strings.HasSuffix(prefix, " ") || len(words) == 0 {
		words = append(words, "")
	}
	var options []string
	switch {
	case len(words) <= 1:
		options = []string{"status", "off", "on", "prefetch", "startup", "engine"}
	case words[0] == "engine":
		options = Engines
	case words[0] == "prefetch" || words[0] == "startup":
		options = []string{"off"}
	}
	last := words[len(words)-1]
	var out []Completion
	for _, name := range options {
		if !strings.HasPrefix(name, last) {
			continue
		}
		value := name
		if len(words) > 1 {
			value = words[0] + " " + name
		}
		out = append(out, Completion{Value: value, Label: name})
	}
	return out
}

// Controller runs `/pi-curator`.
type Controller struct {
	Config  Config
	Curator Options
	// OnEnabledChange applies a durably saved switch to the current sessions without granting consent.
	OnEnabledChange func(bool) error
}

// Handle runs one command line. Every outcome, including failures, goes through notify(message, level).
func (c *Controller) Handle(ctx context.Context, args string, notify func(message, level string)) {
	fields := strings.Fields(args)
	key := "status"
	if len(fields) > 0 {
		key = fields[0]
	}
	if key == "status" {
		if _, err := c.Config.Effective(); err != nil {
			notify(fmt.Sprintf("pi-curator: %v", err), "error")
			return
		}
		lines := append([]string{StatusLine(ctx, c.Curator)}, c.Config.Describe()...)
		lines = append(lines, "settings: "+c.Config.File(), "consent is asked per repository before the first prompt of a session")
		notify(strings.Join(lines, "\n"), "info")
		return
	}
	if (key == "off" || key == "on") && len(fields) == 1 {
		enabled := key == "on"
		if err := SaveSettings(c.Config.File(), Settings{Enabled: &enabled}); err != nil {
			notify(fmt.Sprintf("pi-curator: %v", err), "error")
			return
		}
		if c.OnEnabledChange != nil {
			if err := c.OnEnabledChange(enabled); err != nil {
				notify(fmt.Sprintf("pi-curator: enabled=%t saved, but session switch failed: %v", enabled, err), "error")
				return
			}
		}
		if _, err := c.Config.Effective(); err != nil {
			notify(fmt.Sprintf("pi-curator: enabled=%t saved; settings invalid: %v", enabled, err), "error")
			return
		}
		notify("pi-curator: "+strings.Join(c.Config.Describe(), "; "), "info")
		return
	}
	if len(fields) != 2 {
		notify("pi-curator: "+CommandHelp, "error")
		return
	}
	change, err := ParseSetting(key, fields[1])
	if err == nil {
		err = SaveSettings(c.Config.File(), change)
	}
	if err == nil {
		_, err = c.Config.Effective()
	}
	if err != nil {
		notify(fmt.Sprintf("pi-curator: %v", err), "error")
		return
	}
	notify("pi-curator: "+strings.Join(c.Config.Describe(), "; "), "info")
}
