package betterfooter

import (
	"context"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
)

type registry struct {
	ctx   sdk.Context
	owner *extension
}

func (r registry) BaseURLs(provider string) []string {
	models, err := r.ctx.ModelRegistry().GetAll()
	r.owner.report(r.ctx, err)
	var urls []string
	for _, m := range models {
		if text(m, "provider") == provider {
			urls = append(urls, text(m, "baseUrl"))
		}
	}
	return urls
}
func (r registry) APIKey(provider string) string {
	auth, err := r.ctx.ModelRegistry().GetProviderAuth(provider)
	r.owner.report(r.ctx, err)
	return text(object(auth, "auth"), "apiKey")
}

func (x *extension) worker() {
	defer close(x.done)
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-x.life.Done():
			return
		case <-timer.C:
		case <-x.wake:
		}
		x.mu.Lock()
		ctx := x.ctx
		mounted := x.mounted
		gen := x.generation
		quotaGen := x.quotaGeneration
		git, force := x.gitQueued, x.forceQuota
		x.gitQueued, x.forceQuota = false, false
		cycle := x.pendingCycle
		token := x.cycle
		x.pendingCycle = nil
		x.mu.Unlock()
		if !mounted {
			continue
		}
		if cycle != nil {
			x.skip(ctx, cycle, gen, token)
		}
		if x.life.Err() != nil {
			return
		}
		x.mu.Lock()
		current := x.generation == gen
		x.mu.Unlock()
		if !current {
			continue
		}
		// A credential change need not select a model; follow its quota account on each refresh.
		model := ctx.ModelRegistry().Find(ctx.ModelProvider(), ctx.Model())
		key, err := modelQuotaKey(ctx, model)
		x.report(ctx, err)
		entries, err := ctx.SessionManager().GetEntries()
		x.report(ctx, err)
		displayCwd, cwdErr := ctx.SessionManager().GetCwd()
		x.report(ctx, cwdErr)
		x.mu.Lock()
		if x.generation == gen && cwdErr == nil {
			x.state.Cwd = displayCwd
		}
		x.mu.Unlock()
		usage, usageErr := ctx.GetContextUsage()
		x.report(ctx, usageErr)
		leaf, leafErr := ctx.SessionManager().GetLeafID()
		x.report(ctx, leafErr)
		leafID := ""
		if leaf != nil {
			leafID = *leaf
		}
		// The SDK has no footer branch-change subscription. Poll only the cheap ref
		// query, then read the working tree if the ref changed.
		branch := bf.ReadGitBranch(x.life, ctx.Cwd())
		x.mu.Lock()
		if x.generation == gen && branch != x.state.Branch {
			git = true
		}
		x.mu.Unlock()
		now := time.Now()
		x.mu.Lock()
		if x.generation != gen || x.quotaGeneration != quotaGen {
			x.mu.Unlock()
			continue
		}
		if err == nil {
			x.state.Totals, x.state.LatestHit, x.state.HasHit = x.stats.SummarizeEntries(entries)
		}
		if usageErr == nil && usage != nil {
			x.state.ContextTokens = usage.Tokens
			x.state.ContextWindow = usage.ContextWindow
			x.state.ContextPercent = 0
			if usage.Percent != nil {
				x.state.ContextPercent = *usage.Percent
			}
		} else if usageErr == nil {
			x.state.ContextTokens = nil
		}
		if key != x.state.QuotaKey {
			x.setQuotaKeyLocked(key)
			force = true
		}
		if leafErr == nil && leafID != x.leaf {
			git = true
			x.leaf = leafID
		}
		poll := bf.Polled(key) && (force || now.Sub(x.lastPoll) >= bf.BackoffInterval(bf.PollInterval(key), now.Sub(x.lastActivity)))
		if poll {
			x.lastPoll = now
		}
		quotaGen = x.quotaGeneration
		x.mu.Unlock()
		if git {
			version, err := bf.ReadProjectVersion(ctx.Cwd())
			x.report(ctx, err)
			bounded, cancel := context.WithTimeout(x.life, 30*time.Second)
			changes, ok, err := bf.ReadGitChanges(bounded, ctx.Cwd())
			cancel()
			x.report(ctx, err)
			x.mu.Lock()
			if x.generation == gen {
				x.state.Version = version
				x.state.Branch = branch
				if ok {
					x.state.Git = changes
				} else if err == nil {
					x.state.Git = bf.GitChanges{}
				}
			}
			x.mu.Unlock()
		}
		if poll {
			x.reader.Registry = registry{ctx, x}
			_, _, err := x.reader.Read(x.life, key, force, 0)
			x.report(ctx, err)
		}
		quota, _ := x.store.Get(key)
		x.mu.Lock()
		current = x.generation == gen && x.quotaGeneration == quotaGen
		if current && x.state.QuotaKey == key {
			x.state.Quota = quota
		}
		x.mu.Unlock()
		if current && x.life.Err() == nil {
			x.redraw()
		}
	}
}

// Rendering has its own single coalescing queue: slow quota/git reads must not
// delay the live throughput display or the reset countdown.
func (x *extension) drawLoop() {
	defer close(x.drawDone)
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		checkAccount := false
		select {
		case <-x.life.Done():
			return
		case <-timer.C:
			checkAccount = true
		case <-x.draw:
		}
		x.mu.Lock()
		ctx, mounted := x.ctx, x.mounted
		key := x.state.QuotaKey
		x.mu.Unlock()
		if !mounted {
			continue
		}
		if checkAccount {
			// A slow quota read must not keep an old account visible. Do this only
			// on the timer, not on each streamed-token redraw.
			x.syncModel(ctx)
			x.mu.Lock()
			changed := key != x.state.QuotaKey
			key = x.state.QuotaKey
			x.mu.Unlock()
			if changed {
				x.signal(false, true)
			}
		}
		q, _ := x.store.Get(key)
		x.mu.Lock()
		x.speed.Refresh(time.Since(x.origin))
		x.state.Speed, x.state.Estimated = x.speed.Speed, x.speed.Estimated
		if x.state.QuotaKey == key {
			x.state.Quota = q
		}
		x.mu.Unlock()
		if err := ctx.SetFooterRenderer(x.render(ctx)); err != nil {
			x.report(ctx, err)
		}
	}
}
