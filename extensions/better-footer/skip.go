package betterfooter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
	"github.com/VBenevides/pig-plugins/internal/notice"
)

func ref(model map[string]any) bf.ModelRef {
	return bf.ModelRef{Provider: text(model, "provider"), ModelID: text(model, "id")}
}
func (x *extension) superseded(ctx sdk.Context, selected bf.ModelRef, gen, cycle uint64) bool {
	x.mu.Lock()
	valid := x.generation == gen && x.cycle == cycle && x.settings.SkipExhaustedScopedModels
	x.mu.Unlock()
	return !valid || ctx.ModelProvider() != selected.Provider || ctx.Model() != selected.ModelID || x.life.Err() != nil
}
func configuredThinking(ctx sdk.Context, model bf.ModelRef) (string, error) {
	settings, err := ctx.GetSettings()
	if err != nil {
		return "", err
	}
	// The host settings use provider/modelId keys (upstream settings-manager.ts:900).
	if levels := object(settings, "modelThinkingLevels"); levels != nil {
		if level := text(levels, model.Provider+"/"+model.ModelID); bf.IsThinkingLevel(level) {
			return level, nil
		}
	}
	level := text(settings, "defaultThinkingLevel")
	if bf.IsThinkingLevel(level) {
		return level, nil
	}
	return "", nil
}
func (x *extension) skip(ctx sdk.Context, event map[string]any, gen, cycle uint64) {
	selected, previous := ref(object(event, "model")), ref(object(event, "previousModel"))
	if text(object(event, "model"), "api") == "pi-virtual" {
		return
	}
	scope, err := ctx.ScopedModels()
	if err != nil {
		x.report(ctx, err)
		return
	}
	if len(scope) < 2 {
		return
	}
	models := make([]bf.ModelRef, len(scope))
	start := -1
	for i, item := range scope {
		models[i] = ref(item.Model)
		if models[i] == selected {
			start = i
		}
	}
	if start < 0 {
		return
	}
	direction := bf.CycleDirection(models, previous, selected)
	// Prefetch distinct accounts with at most four simultaneous bounded readers. The
	// traversal itself remains in selection order; unknown quota never blocks a model.
	keys := make([]string, len(scope))
	accounts, err := ctx.OAuthAccounts()
	if err != nil {
		x.report(ctx, fmt.Errorf("native OAuth account enumeration failed"))
		return // Unknown credentials cannot justify skipping any scoped model.
	}
	for i, item := range scope {
		keys[i], err = modelQuotaKeyWithSource(ctx, item.Model, accountSnapshot(accounts))
		if err != nil {
			x.report(ctx, err)
		}
	}
	bounded, cancel := context.WithTimeout(x.life, 30*time.Second)
	defer cancel()
	x.reader.Registry = registry{ctx, x}
	type reading struct {
		done  chan struct{}
		quota bf.ProviderQuota
		known bool
		err   error
	}
	reads := map[string]*reading{}
	jobs := make(chan string, len(keys))
	for _, key := range keys {
		if key == "" || reads[key] != nil {
			continue
		}
		reads[key] = &reading{done: make(chan struct{})}
		jobs <- key
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(reads)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for key := range jobs {
				r := reads[key]
				r.quota, r.known, r.err = x.reader.Read(bounded, key, false, bf.QuotaCacheAge)
				close(r.done)
			}
		}()
	}
	defer func() { cancel(); workers.Wait() }()
	skipped := []string{}
	for step := range len(scope) {
		index := (start + step*direction + len(scope)) % len(scope)
		item := scope[index]
		candidate := models[index]
		key := keys[index]
		q, known := bf.ProviderQuota{}, false
		if r := reads[key]; r != nil {
			select {
			case <-r.done:
				if r.err == nil {
					q, known = r.quota, r.known
				}
				x.report(ctx, r.err)
			case <-bounded.Done():
			}
		}
		if x.superseded(ctx, selected, gen, cycle) {
			return
		}
		// The selected model and this candidate must still use the same accounts
		// captured for these reads. Account changes do not require model_select.
		for _, check := range []int{start, index} {
			currentKey, err := modelQuotaKey(ctx, scope[check].Model)
			if err != nil {
				x.report(ctx, err)
				return
			}
			if currentKey != keys[check] {
				return
			}
		}
		if bf.IsQuotaExhausted(q, known, time.Now()) {
			skipped = append(skipped, candidate.Provider+"/"+candidate.ModelID+" (quota exhausted)")
			continue
		}
		if candidate == selected {
			return
		}
		ok, err := ctx.SetModel(candidate.Provider + "/" + candidate.ModelID)
		if err != nil {
			x.report(ctx, err)
		}
		if err != nil || !ok {
			skipped = append(skipped, candidate.Provider+"/"+candidate.ModelID+" (unavailable)")
			continue
		}
		x.mu.Lock()
		valid := x.generation == gen && x.settings.SkipExhaustedScopedModels
		x.mu.Unlock()
		if !valid || ctx.ModelProvider() != candidate.Provider || ctx.Model() != candidate.ModelID {
			return
		}
		level := item.ThinkingLevel
		if level == "" {
			var err error
			level, err = configuredThinking(ctx, candidate)
			x.report(ctx, err)
		}
		if level == "" {
			level = text(event, "preCycleThinking")
		}
		if level != "" {
			ctx.SetThinkingLevel(level)
		}
		x.report(ctx, x.remember(ctx))
		x.syncModel(ctx)
		name := text(item.Model, "name")
		if name == "" {
			name = candidate.ModelID
		}
		effort, err := ctx.GetThinkingLevel()
		x.report(ctx, err)
		if reasoning, _ := item.Model["reasoning"].(bool); reasoning && effort != "off" {
			name += " (thinking: " + effort + ")"
		}
		notice.Show(ctx, "Switched to "+bf.Sanitize(name)+"; skipped "+strings.Join(skipped, ", "), "info")
		return
	}
	if x.superseded(ctx, selected, gen, cycle) {
		return
	}
	restored := false
	if previous.Provider != "" {
		var err error
		restored, err = ctx.SetModel(previous.Provider + "/" + previous.ModelID)
		x.report(ctx, err)
		if restored && ctx.ModelProvider() == previous.Provider && ctx.Model() == previous.ModelID {
			if level := text(event, "preCycleThinking"); level != "" {
				ctx.SetThinkingLevel(level)
			}
			x.report(ctx, x.remember(ctx))
			x.syncModel(ctx)
		}
	}
	message := "No scoped model with available quota was found (" + strings.Join(skipped, ", ") + ")"
	if restored {
		message += "; staying on " + previous.Provider + "/" + previous.ModelID
	}
	notice.Show(ctx, message, "warning")
}
