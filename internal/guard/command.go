package guard

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// CommandName is the slash command the extension registers.
const CommandName = "smart-approve-lancet"

// Help texts of the command.
const (
	CommandHelp = "usage: /smart-approve-lancet [interactive|strict|status] | lancet [status|setup|on|off|check <command>] (no argument toggles the mode)"
	LancetHelp  = "usage: /smart-approve-lancet lancet status | setup | on | off | check <command>"
)

// Notify shows a message to the user; level is "info", "warning" or "error".
type Notify func(message, level string)

// Controller runs the /smart-approve-lancet command against a gate and the local LANCET service.
type Controller struct {
	Gate     *Gate
	Lancet   Lancet
	Settings string // path of the settings file
}

// Chip is the footer text of the current state.
func (c *Controller) Chip() string {
	return fmt.Sprintf("%s %s - lancet %s", CommandName, c.Gate.Mode(), onOff(c.Gate.LancetOn()))
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// Check judges a call using the current persisted settings. Another session or
// command handler can update the same file after this controller was created;
// its startup snapshot must not override an explicitly saved interactive mode.
// An unreadable or invalid file blocks the call without changing the last valid
// runtime state, so a configuration failure cannot disable mandatory scoring.
func (c *Controller) Check(ctx context.Context, call Call, announce func()) Decision {
	if !Gated(call.Tool) {
		return allow
	}
	settings := LoadSettings(c.Settings)
	if settings.Problem != "" {
		return block("blocked %s; cannot load current settings: %s.", call.Tool, settings.Problem)
	}
	if c.Gate.applySettings(settings) && announce != nil {
		announce()
	}
	return c.Gate.Check(ctx, call)
}

// Handle runs one command line. announce is called after the mode or LANCET state changed.
func (c *Controller) Handle(ctx context.Context, args string, notify Notify, announce func()) {
	trimmed := strings.TrimSpace(args)
	word := strings.ToLower(trimmed)
	switch {
	case word == "lancet" || strings.HasPrefix(word, "lancet "):
		c.lancetCommand(ctx, trimmed[len("lancet"):], notify, announce)
		return
	case word == "status":
		notify(c.status(), "info")
		return
	}
	next := word
	if word == "" {
		next = string(Interactive)
		if c.Gate.Mode() == Interactive {
			next = string(Strict)
		}
	}
	mode, ok := ParseMode(next)
	if !ok {
		notify(fmt.Sprintf(`%sunknown option "%s"; %s`, Prefix, word, CommandHelp), "error")
		return
	}
	if err := SaveSettings(c.Settings, Change{Mode: new(mode)}); err != nil {
		notify(fmt.Sprintf("%smode not changed; cannot save %s: %v", Prefix, c.Settings, err), "error")
		return
	}
	c.Gate.SetMode(mode)
	announce()
	notify(fmt.Sprintf("%s: %s - lancet %s", CommandName, mode, onOff(c.Gate.LancetOn())), "info")
}

func (c *Controller) status() string {
	return strings.Join([]string{
		fmt.Sprintf("%s: %s - lancet %s", CommandName, c.Gate.Mode(), onOff(c.Gate.LancetOn())),
		"interactive asks before dangerous commands and protected paths; strict blocks them without asking",
		"hard-blocked commands are blocked in both modes; see /smart-approve-lancet lancet status for the local model",
		"LLM risk analysis and auto mode are not part of this port",
		"settings: " + c.Settings,
	}, "\n")
}

func (c *Controller) lancetStatus() string {
	lines := []string{fmt.Sprintf("LANCET: %s", strings.ToUpper(onOff(c.Gate.LancetOn())))}
	model := c.Lancet.Model()
	switch {
	case model.Verified:
		loaded := "loads on the first bash command"
		if c.Lancet.Loaded() {
			loaded = "loaded"
		}
		lines = append(lines, fmt.Sprintf("model: verified at %s (%s)", model.Directory, loaded))
	case model.Installed:
		lines = append(lines, "model: damaged (checksum mismatch); run /smart-approve-lancet lancet setup")
	default:
		problem := model.Problem
		if problem == "" {
			problem = "not downloaded"
		}
		lines = append(lines, "model: "+problem+"; run /smart-approve-lancet lancet setup")
	}
	if model.RuntimeProblem != "" {
		lines = append(lines, "runtime: unavailable ("+model.RuntimeProblem+")")
	} else {
		lines = append(lines, "runtime: "+model.Runtime)
	}
	return strings.Join(append(lines,
		"policy (bash only, after hard blocks):",
		"  NOT_FLAGGED -> continues to the dangerous-command check",
		"  REVIEW      -> asks you (blocked in strict mode or without a UI)",
		"  RISKY       -> blocked",
		"  unavailable -> blocked while LANCET is on",
	), "\n")
}

func (c *Controller) lancetCommand(ctx context.Context, rest string, notify Notify, announce func()) {
	words := strings.Fields(rest)
	action := ""
	if len(words) > 0 {
		action = strings.ToLower(words[0])
		words = words[1:]
	}
	switch action {
	case "status":
		notify(c.lancetStatus(), "info")
	case "setup":
		c.setup(ctx, notify)
	case "on":
		c.enable(ctx, notify, announce)
	case "off":
		c.disable(notify, announce)
	case "check":
		command := strings.Join(words, " ")
		if command == "" {
			notify("usage: /smart-approve-lancet lancet check <command>", "error")
			return
		}
		verdict, err := c.Lancet.Score(ctx, command)
		if err != nil {
			notify(fmt.Sprintf("LANCET: check unavailable: %v", err), "error")
			return
		}
		message := fmt.Sprintf("LANCET: %s, score=%s", verdict.Classification, ScoreText(verdict.Score))
		if verdict.Reason != "" {
			message += ", reason=" + verdict.Reason
		}
		notify(fmt.Sprintf("%s; command=%s (not executed)", message, truncateCommand(command)), "info")
	default:
		notify(LancetHelp, "info")
	}
}

// truncateCommand shortens a command to 120 characters (UTF-16 units in the original; code points here) with "...".
func truncateCommand(command string) string {
	if utf8.RuneCountInString(command) <= 120 {
		return command
	}
	return string([]rune(command)[:117]) + "..."
}

func (c *Controller) setup(ctx context.Context, notify Notify) {
	installed, err := c.Lancet.Setup(ctx, func(string) {
		notify("LANCET: downloading and verifying the pinned files; this can take a few minutes.", "info")
	})
	switch {
	case err != nil:
		notify(fmt.Sprintf("LANCET: setup failed: %v", err), "error")
	case len(installed) == 0:
		notify("LANCET: the model and the ONNX Runtime library are already installed and verified.", "info")
	default:
		notify(fmt.Sprintf("LANCET: %s downloaded, verified and installed. Enable it with /smart-approve-lancet lancet on.",
			strings.Join(installed, " and ")), "info")
	}
}

func (c *Controller) enable(ctx context.Context, notify Notify, announce func()) {
	if !c.Lancet.Model().Verified {
		notify("LANCET: cannot enable: the pinned model is not verified. Run /smart-approve-lancet lancet setup first.", "error")
		return
	}
	// Scoring remains mandatory for commands outside the read-only subset.
	if _, err := c.Lancet.Score(ctx, "echo lancet-self-test"); err != nil {
		notify(fmt.Sprintf("LANCET: cannot enable: scoring does not work (%v). Bash stays as it was.", err), "error")
		return
	}
	if err := SaveSettings(c.Settings, Change{Lancet: new(true)}); err != nil {
		notify(fmt.Sprintf("LANCET: not enabled; cannot save %s: %v", c.Settings, err), "error")
		return
	}
	c.Gate.SetLancet(true)
	announce()
	notify("LANCET: on. Commands outside the read-only subset are scored locally; if LANCET becomes unavailable, those commands are blocked.", "info")
}

func (c *Controller) disable(notify Notify, announce func()) {
	if err := SaveSettings(c.Settings, Change{Lancet: new(false)}); err != nil {
		notify(fmt.Sprintf("LANCET: not changed; cannot save %s: %v", c.Settings, err), "error")
		return
	}
	c.Gate.SetLancet(false)
	announce()
	if err := c.Lancet.Release(); err != nil {
		notify(fmt.Sprintf("LANCET is off, but releasing the runtime failed: %v", err), "warning")
	}
	notify("LANCET: off. Bash uses the pattern checks only.", "info")
}

// Completion is one argument completion.
type Completion struct {
	Value string
	Label string
}

var whitespace = regexp.MustCompile(`\s+`)

// Completions returns the completions for the text after "/smart-approve-lancet ".
func Completions(prefix string) []Completion {
	words := whitespace.Split(prefix, -1)
	last := words[len(words)-1]
	var options []string
	switch {
	case len(words) <= 1:
		options = []string{string(Interactive), string(Strict), "status", "lancet"}
	case words[0] == "lancet" && len(words) == 2:
		options = []string{"status", "setup", "on", "off", "check"}
	}
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
