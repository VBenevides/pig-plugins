package contextprune

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	prune "github.com/VBenevides/pig-plugins/internal/contextprune"
)

func (st *state) skillCommand(ctx sdk.Context, args string) (string, error) {
	words := strings.Fields(args)
	if len(words) < 2 || len(words) > 3 {
		return "usage: /context-prune skills list|finish|keep|active|release [invocation-id]", nil
	}
	action := words[1]
	if action == "expire-after" {
		if len(words) != 3 {
			return "usage: /context-prune skills expire-after <off|1-10000>", nil
		}
		age := 0
		if words[2] != "off" {
			value, err := strconv.Atoi(words[2])
			if err != nil || value < 1 || value > 10000 {
				return "", fmt.Errorf("skill expiry must be off or an integer from 1 to 10000")
			}
			age = value
		}
		if err := prune.SetSkillMaxAge(prune.File(os.Getenv), age); err != nil {
			return "", err
		}
		st.startRun(ctx, prune.File(os.Getenv))
		return fmt.Sprintf("context-prune: released skill expiry is %d user interactions (0 means off). Protected skills never expire.", age), nil
	}
	if action != "list" && action != "finish" && action != "keep" && action != "active" && action != "release" {
		return "unknown skill action", nil
	}
	entries, err := ctx.SessionManager().GetBranch(nil)
	if err != nil {
		return "", fmt.Errorf("read skill lifetimes: %w", err)
	}
	invocations := prune.Lifetimes(entries)
	if action == "list" {
		if len(words) != 2 {
			return "usage: /context-prune skills list", nil
		}
		if len(invocations) == 0 {
			return "context-prune: no skill envelopes in the active branch.", nil
		}
		var lines []string
		// Bound the UI output. Older invocations remain addressable by exact ID.
		start := max(0, len(invocations)-50)
		for _, inv := range invocations[start:] {
			lines = append(lines, fmt.Sprintf("%s [%s] %s", inv.Name, inv.State, inv.ID))
		}
		return strings.Join(lines, "\n"), nil
	}
	id := ""
	if len(words) == 3 {
		id = words[2]
	} else {
		if len(invocations) == 0 {
			return "context-prune: no skill envelopes in the active branch.", nil
		}
		before, err := branchIdentity(ctx)
		if err != nil {
			return "", err
		}
		options := []string{}
		ids := map[string]string{}
		for _, inv := range invocations[max(0, len(invocations)-50):] {
			option := fmt.Sprintf("%s [%s] %s", inv.Name, inv.State, inv.ID)
			options = append(options, option)
			ids[option] = inv.ID
		}
		selected, ok, err := ctx.Select("Select the skill invocation to "+action, options)
		if err != nil {
			return "", fmt.Errorf("select skill invocation: %w", err)
		}
		if !ok {
			return "context-prune: no skill state changed.", nil
		}
		id = ids[selected]
		after, err := branchIdentity(ctx)
		if err != nil {
			return "", err
		}
		if before != after {
			return "", fmt.Errorf("session or branch changed during skill selection; try again")
		}
		// Re-read after a dialog so navigation cannot persist a stale branch choice.
		entries, err = ctx.SessionManager().GetBranch(nil)
		if err != nil {
			return "", fmt.Errorf("recheck skill lifetimes: %w", err)
		}
	}
	data, err := prune.SkillTransition(entries, id, action)
	if err != nil {
		return "", err
	}
	if err := ctx.AppendEntry(prune.SkillStateType, data); err != nil {
		return "", fmt.Errorf("persist skill lifetime: %w", err)
	}
	st.startRun(ctx, prune.File(os.Getenv))
	return fmt.Sprintf("context-prune: %s is %s. This changes retention only, not skill execution permission.", id, data["state"]), nil
}

func branchIdentity(ctx sdk.Context) (string, error) {
	session, err := ctx.GetSessionID()
	if err != nil {
		return "", fmt.Errorf("read skill session identity: %w", err)
	}
	leaf, err := ctx.GetLeafID()
	if err != nil {
		return "", fmt.Errorf("read skill branch identity: %w", err)
	}
	id := ""
	if leaf != nil {
		id = *leaf
	}
	return session + "\x00" + id, nil
}

func explicitInteraction(data map[string]any) bool {
	source, _ := data["source"].(string)
	text, _ := data["text"].(string)
	behavior, _ := data["streamingBehavior"].(string)
	return (source == "interactive" || source == "rpc") && behavior == "" && strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), "/")
}
