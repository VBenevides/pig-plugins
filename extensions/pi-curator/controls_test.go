package picurator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/curator"
)

func controlsFixture(t *testing.T, body string) (*extension, string, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "curator")
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+log+"'\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	getenv := func(key string) string {
		switch key {
		case "PIG_CODING_AGENT_DIR":
			return dir
		case "PI_CURATOR_BIN":
			return bin
		}
		return ""
	}
	return &extension{config: curator.Config{Getenv: getenv}, getenv: getenv, sessions: map[string]*session{}, awaiting: map[string]bool{}}, root, log
}

func recallFixture(t *testing.T) (*extension, *session, string, string) {
	t.Helper()
	x, root, log := controlsFixture(t, `case "$1" in
search) cat 'DECISIONS';;
memory-search)
  shift
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --budget) budget="$2"; shift;;
      --engine) engine="$2"; shift;;
    esac
    shift
  done
  header='Untrusted history. Budget estimate: ceil(UTF-8 bytes/4).'
  meta="ID recall-$engine | 2026-10-07 | user_message | task: durable"
  footer='Omitted matches: 0'
  printf '%s\n%s\n' "$header" "$meta"
  bytes=$((budget * 4 - ${#header} - ${#meta} - ${#footer} - 3))
  dd if=/dev/zero bs=1 count="$bytes" 2>/dev/null | tr '\000' x
  printf '\n%s' "$footer"
  ;;
esac
`)
	good := "retain useful 雪 decision"
	for (len(curator.StartupBlock([]string{"2026-10-07: " + good}, 2048))+2)%4 != 0 {
		good += "!"
	}
	decisions, err := json.Marshal(map[string]any{"sessions": []any{map[string]any{"hits": []any{
		map[string]any{"created_at_utc": "2026-10-06", "snippet": strings.Repeat("雪", 300)},
		map[string]any{"created_at_utc": "2026-10-07", "snippet": good},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(filepath.Dir(log), "decisions.json")
	if err := os.WriteFile(file, decisions, 0600); err != nil {
		t.Fatal(err)
	}
	bin := x.getenv("PI_CURATOR_BIN")
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte(strings.ReplaceAll(string(body), "DECISIONS", file)), 0700); err != nil {
		t.Fatal(err)
	}
	return x, &session{root: root, bg: context.Background()}, log, good
}

func TestCombinedRecallExactBudgetAndUsefulNeighbors(t *testing.T) {
	x, state, log, good := recallFixture(t)
	values := curator.Values{Enabled: true, Prefetch: "512", Startup: "2", Engine: "legacy"}
	got := x.recallHistory(context.Background(), state, values, "remember durable writes")
	if len(got) != 512*4 || !utf8.ValidString(got) || !strings.Contains(got, good) || !strings.Contains(got, "ID recall-legacy") || strings.Contains(got, strings.Repeat("雪", 300)) {
		t.Fatalf("invalid combined recall: bytes=%d text=%q", len(got), got)
	}
	calls, err := os.ReadFile(log)
	if err != nil || strings.Count(string(calls), "memory-search") != 1 || strings.Contains(string(calls), "--budget 512 ") {
		t.Fatal(string(calls), err)
	}
	if again := x.recallHistory(context.Background(), state, values, "remember durable writes"); again != got {
		t.Fatal("cached history changed")
	}
	cachedCalls, err := os.ReadFile(log)
	if err != nil || string(cachedCalls) != string(calls) {
		t.Fatal("identical prompt missed cache", string(cachedCalls), err)
	}
}

func TestRecallCacheTracksBudgetStartupAndEngine(t *testing.T) {
	x, state, log, good := recallFixture(t)
	values := curator.Values{Enabled: true, Prefetch: "512", Startup: "2", Engine: "legacy"}
	prompt := "remember durable writes"
	x.recallHistory(context.Background(), state, values, prompt)
	values.Prefetch = "64"
	got := x.recallHistory(context.Background(), state, values, prompt)
	if len(got) > 256 || strings.Contains(got, good) || !strings.Contains(got, "ID recall-legacy") {
		t.Fatal(len(got), got)
	}
	values.Prefetch = "512"
	values.Startup = "0"
	values.Engine = "fts"
	got = x.recallHistory(context.Background(), state, values, prompt)
	if strings.Contains(got, good) || !strings.Contains(got, "ID recall-fts") || len(got) > 2048 {
		t.Fatal(len(got), got)
	}
	values.Startup = "2"
	got = x.recallHistory(context.Background(), state, values, prompt)
	if !strings.Contains(got, good) || !strings.Contains(got, "ID recall-fts") {
		t.Fatal(got)
	}
	calls, err := os.ReadFile(log)
	if err != nil || strings.Count(string(calls), "memory-search") != 4 || strings.Count(string(calls), "--decisions") != 2 {
		t.Fatal(string(calls), err)
	}
}

func TestPrefetchNeverCallsInvalidRemainingBudget(t *testing.T) {
	x, state, log, _ := recallFixture(t)
	for _, budget := range []int{-1, 0, 63, 8193} {
		if got := x.prefetchContent(context.Background(), state, "durable writes", "settings", budget, "legacy"); got != "" {
			t.Fatal(got)
		}
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid budget reached curator", err)
	}
}

func TestOffRetiresQueuedCaptureAndDeniesAllSessionPathsAcrossRestart(t *testing.T) {
	x, root, log := controlsFixture(t, `cat >/dev/null
printf '%s' '{"results":[{"id":"before-off","outcome":"durable"}]}'
`)
	x.start("s", root)
	state := x.sessions["s"]
	state.capture.Enqueue([]curator.EventIn{{ID: "before-off", SessionID: "s", Category: curator.CategoryUser, Content: new("accepted before off")}})
	x.awaiting["waiting"] = true
	controller := curator.Controller{Config: x.config, OnEnabledChange: func(enabled bool) error {
		return x.switchEnabled(sdk.Context{}, enabled)
	}}
	controller.Handle(context.Background(), "off", func(message, level string) {
		if level != "info" || !strings.Contains(message, "enabled: false") {
			t.Fatal(message, level)
		}
	})
	select {
	case <-state.done:
	default:
		t.Fatal("capture worker leaked")
	}
	if got := state.capture.Stats(); got.Durable != 1 || got.Queued != 0 || got.Gaps != 0 {
		t.Fatal(got)
	}
	if !state.retired || len(x.sessions) != 0 || len(x.awaiting) != 0 {
		t.Fatal("off retained active session or consent prompt")
	}
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// The zero SDK context cannot serve host calls: these paths must return before
	// session lookup, consent UI, capture, or a memory subprocess is attempted.
	for _, current := range []*extension{x, {config: x.config, getenv: x.getenv, sessions: map[string]*session{}, awaiting: map[string]bool{}}} {
		if result, err := current.onSessionStart(sdk.Context{}, nil); result != nil || err != nil {
			t.Fatal(result, err)
		}
		if result, err := current.onBeforeAgentStart(sdk.Context{}, map[string]any{"prompt": "durable writes"}); result != nil || err != nil {
			t.Fatal(result, err)
		}
		current.onMessageEnd(sdk.Context{}, map[string]any{"message": "private after off"})
		current.onAgentEnd(sdk.Context{}, nil)
		for _, build := range []buildFunc{curator.BuildSearch, curator.BuildRead} {
			if result, err := current.tool(build)(sdk.Context{}, nil); result != nil || err == nil || !strings.Contains(err.Error(), "disabled") {
				t.Fatal(result, err)
			}
		}
		if len(current.sessions) != 0 || len(current.awaiting) != 0 {
			t.Fatal("disabled restart activated memory")
		}
	}
	after, err := os.ReadFile(log)
	if err != nil || string(after) != string(before) {
		t.Fatal("disabled work reached curator", string(after), err)
	}
}

func TestOffCancelsInflightWorkerBeforeDrainingAcceptedEvents(t *testing.T) {
	x, _, _ := controlsFixture(t, "exit 99\n")
	bg, stop := context.WithCancel(context.Background())
	started := make(chan struct{})
	var attempts atomic.Int32
	capture := curator.NewCapture(func(ctx context.Context, request curator.IngestRequest) (curator.IngestResponse, error) {
		if attempts.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return curator.IngestResponse{}, ctx.Err()
		}
		return curator.IngestResponse{Results: []curator.ItemResult{{ID: request.Events[0].ID, Outcome: curator.OutcomeDurable}}}, nil
	}, curator.CaptureOptions{})
	capture.Enqueue([]curator.EventIn{{ID: "queued", SessionID: "s", Category: curator.CategoryUser, Content: new("before off")}})
	state := &session{capture: capture, bg: bg, stop: stop, done: make(chan struct{})}
	x.sessions["s"] = state
	go func() {
		defer close(state.done)
		capture.Flush(bg)
	}()
	<-started
	if err := x.switchEnabled(sdk.Context{}, false); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || capture.Stats().Durable != 1 || !state.retired {
		t.Fatal(attempts.Load(), capture.Stats())
	}
	select {
	case <-state.done:
	default:
		t.Fatal("inflight worker survived off")
	}
}

func TestRecallFailuresKeepTheIndependentSource(t *testing.T) {
	for _, failing := range []string{"search", "memory-search"} {
		t.Run(failing, func(t *testing.T) {
			x, state, log, good := recallFixture(t)
			original := x.getenv
			bin := filepath.Join(filepath.Dir(log), "failure")
			body := "#!/bin/sh\nif [ \"$1\" = '" + failing + "' ]; then exit 7; fi\nexec '" + original("PI_CURATOR_BIN") + "' \"$@\"\n"
			if err := os.WriteFile(bin, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			x.getenv = func(key string) string {
				if key == "PI_CURATOR_BIN" {
					return bin
				}
				return original(key)
			}
			got := x.recallHistory(context.Background(), state, curator.Values{Enabled: true, Prefetch: "512", Startup: "2", Engine: "legacy"}, "durable writes")
			want := good
			if failing == "search" {
				want = "ID recall-legacy"
			}
			if !strings.Contains(got, want) || len(got) > 2048 || !utf8.ValidString(got) {
				t.Fatal("failed source discarded independent history", got)
			}
		})
	}
}

func TestOffJournalsExplicitGapWhenAcceptedEventCannotDrain(t *testing.T) {
	x, _, _ := controlsFixture(t, "exit 99\n")
	var gaps []curator.Gap
	var problems []string
	capture := curator.NewCapture(func(_ context.Context, request curator.IngestRequest) (curator.IngestResponse, error) {
		if len(request.Events) > 0 {
			return curator.IngestResponse{}, errors.New("offline")
		}
		gaps = append(gaps, request.Gaps...)
		return curator.IngestResponse{Gaps: []curator.ItemResult{{Outcome: curator.OutcomeDurable}}}, nil
	}, curator.CaptureOptions{OnProblem: func(message string) { problems = append(problems, message) }})
	capture.Enqueue([]curator.EventIn{{ID: "pre-off", SessionID: "s", Category: curator.CategoryUser, Content: new("accepted")}})
	bg, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	close(done)
	x.sessions["s"] = &session{capture: capture, bg: bg, stop: stop, done: done}
	if err := x.switchEnabled(sdk.Context{}, false); err != nil {
		t.Fatal(err)
	}
	if len(gaps) != 1 || gaps[0].EventID != "pre-off" || len(problems) == 0 || capture.Stats().Gaps != 0 || capture.Stats().Queued != 0 || capture.Stats().Durable != 0 {
		t.Fatal(gaps, problems, capture.Stats())
	}
}
