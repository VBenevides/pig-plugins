package guard

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/VBenevides/pig-plugins/internal/hashline"
	"github.com/VBenevides/pig-plugins/internal/lancet"
)

// ScoreTimeout bounds one LANCET score, as in the TypeScript implementation.
const ScoreTimeout = 60 * time.Second

// Prefix starts every reason the gate returns, so the model and the user can tell where a block came from.
const Prefix = "smart-approve-lancet: "

// Scorer scores a bash command. An error means the verdict is unavailable and the gate fails closed.
type Scorer interface {
	Score(ctx context.Context, command string) (lancet.Result, error)
}

// Call is one tool call to judge.
type Call struct {
	// Tool is the tool name; only bash, write and edit are gated.
	Tool string
	// Input is the tool's arguments.
	Input map[string]any
	// Cwd resolves relative paths.
	Cwd string
	// HasUI is true when a dialog can be shown.
	HasUI bool
	// Confirm shows a yes/no dialog. A returned error counts as a refusal.
	Confirm func(title, body string) (bool, error)
}

// Decision is the verdict on one call.
type Decision struct {
	Block bool
	// Reason is the text the model sees when Block is true.
	Reason string
}

var allow = Decision{}

func block(format string, args ...any) Decision {
	return Decision{Block: true, Reason: Prefix + fmt.Sprintf(format, args...)}
}

// Gated reports whether the gate judges calls of this tool.
func Gated(tool string) bool { return tool == "bash" || tool == "write" || tool == "edit" }

// Gate is the safety policy for bash, write and edit. It is safe for concurrent use.
type Gate struct {
	matcher *PathMatcher
	scorer  Scorer

	mu       sync.RWMutex
	mode     Mode
	lancetOn bool
}

// NewGate creates a gate with the loaded settings. scorer may be nil only while LANCET is off.
func NewGate(settings Settings, scorer Scorer) *Gate {
	return &Gate{matcher: DefaultPathMatcher(), scorer: scorer, mode: settings.Mode, lancetOn: settings.Lancet}
}

// Mode returns the approval mode.
func (g *Gate) Mode() Mode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.mode
}

// LancetOn reports whether every bash command is scored.
func (g *Gate) LancetOn() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lancetOn
}

// SetMode changes the approval mode.
func (g *Gate) SetMode(mode Mode) {
	g.mu.Lock()
	g.mode = mode
	g.mu.Unlock()
}

// SetLancet turns LANCET scoring on or off.
func (g *Gate) SetLancet(on bool) {
	g.mu.Lock()
	g.lancetOn = on
	g.mu.Unlock()
}

// applySettings makes the persisted mode and scoring switch effective together.
// It reports whether the footer needs to be updated.
func (g *Gate) applySettings(settings Settings) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	changed := g.mode != settings.Mode || g.lancetOn != settings.Lancet
	g.mode, g.lancetOn = settings.Mode, settings.Lancet
	return changed
}

// ScoreText formats a score like the TypeScript original: four decimals, or "n/a".
func ScoreText(score *float64) string {
	if score == nil || math.IsNaN(*score) || math.IsInf(*score, 0) {
		return "n/a"
	}
	return fmt.Sprintf("%.4f", *score)
}

// Check judges one call. Calls of other tools are allowed. Any failure to judge a gated call blocks it.
func (g *Gate) Check(ctx context.Context, call Call) (decision Decision) {
	if !Gated(call.Tool) {
		return allow
	}
	defer func() {
		if r := recover(); r != nil {
			decision = block("blocked %s; policy evaluation failed: %v.", call.Tool, r)
		}
	}()
	var err error
	if call.Tool == "bash" {
		decision, err = g.checkBash(ctx, call)
	} else {
		decision, err = g.checkWrite(call)
	}
	if err != nil {
		return block("blocked %s; policy evaluation failed: %v.", call.Tool, err)
	}
	return decision
}

// cannotAsk says why the user cannot be asked, or "" when a dialog can be shown.
func (g *Gate) cannotAsk(call Call) string {
	switch {
	case g.Mode() == Strict:
		return "strict mode blocks it without asking"
	case !call.HasUI || call.Confirm == nil:
		return "no UI is available to confirm it"
	}
	return ""
}

// confirm asks; a broken dialog never reads as approval.
func confirm(call Call, title, body string) bool {
	approved, err := call.Confirm(title, body)
	return err == nil && approved
}

func (g *Gate) checkBash(ctx context.Context, call Call) (Decision, error) {
	command, ok := call.Input["command"].(string)
	if !ok {
		return block("blocked bash call without a string command."), nil
	}
	analysis, err := Analyze(command)
	if err != nil {
		return Decision{}, err
	}
	labels := analysis.Labels
	if analysis.HardBlocked {
		return block("blocked hard-blocked command (%s). This operation is never allowed.", strings.Join(labels, ", ")), nil
	}
	if g.LancetOn() {
		if g.scorer == nil {
			return block("blocked bash; LANCET is on but unavailable (no scorer). " +
				"Run /smart-approve-lancet lancet status, or /smart-approve-lancet lancet off."), nil
		}
		scoreCtx, cancel := context.WithTimeout(ctx, ScoreTimeout)
		verdict, err := g.scorer.Score(scoreCtx, command)
		cancel()
		if err != nil {
			return block("blocked bash; LANCET is on but unavailable (%v). "+
				"Run /smart-approve-lancet lancet status, or /smart-approve-lancet lancet off.", err), nil
		}
		switch verdict.Classification {
		case lancet.Risky:
			return block("blocked command that LANCET flagged as risky (score=%s).", ScoreText(verdict.Score)), nil
		case lancet.Review:
			label := "LANCET review, score " + ScoreText(verdict.Score)
			if verdict.Reason != "" {
				label += ": " + verdict.Reason
			}
			labels = append(append([]string(nil), labels...), label)
		case lancet.NotFlagged:
		default:
			return block("blocked bash; LANCET is on but unavailable (LANCET returned an invalid verdict). " +
				"Run /smart-approve-lancet lancet status, or /smart-approve-lancet lancet off."), nil
		}
	}
	if len(labels) == 0 {
		return allow, nil
	}
	joined := strings.Join(labels, ", ")
	if why := g.cannotAsk(call); why != "" {
		return block("blocked dangerous command (%s); %s.", joined, why), nil
	}
	if !confirm(call, "Dangerous command: "+joined, "Risk Description: "+joined+"\n\n"+command+"\n\nAllow this command to run?") {
		return block("user denied dangerous command (%s).", joined), nil
	}
	return allow, nil
}

func (g *Gate) checkWrite(call Call) (Decision, error) {
	raw, ok := call.Input["path"].(string)
	if !ok || raw == "" {
		return block("blocked %s call without a string path.", call.Tool), nil
	}
	cwd := call.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	absolute := hashline.ResolveToolPath(raw, cwd)
	candidates := []string{absolute}
	if real := RealTarget(absolute); real != "" && real != absolute {
		candidates = append(candidates, real)
	}
	protected := false
	for _, candidate := range candidates {
		if g.matcher.IsProtected(candidate) {
			protected = true
			break
		}
	}
	if !protected {
		return allow, nil
	}
	if why := g.cannotAsk(call); why != "" {
		return block("blocked %s to protected path %s; %s.", call.Tool, absolute, why), nil
	}
	body := fmt.Sprintf("Risk Description: %s modifies a protected file.\n\n%s wants to modify a protected file.\n\nPath: %s\n\nAllow this change?", call.Tool, call.Tool, absolute)
	if !confirm(call, "Protected path: "+absolute, body) {
		return block("user denied %s to protected path %s.", call.Tool, absolute), nil
	}
	return allow, nil
}
