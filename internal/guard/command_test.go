package guard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/VBenevides/pig-plugins/internal/lancet"
)

type fakeLancet struct {
	fakeScorer
	model    ModelInfo
	loaded   bool
	setupErr error
	setupOut []string
	released int
	relErr   error
}

func (f *fakeLancet) Model() ModelInfo { return f.model }
func (f *fakeLancet) Loaded() bool     { return f.loaded }
func (f *fakeLancet) Setup(_ context.Context, progress func(string)) ([]string, error) {
	progress("downloading")
	return f.setupOut, f.setupErr
}
func (f *fakeLancet) Release() error { f.released++; return f.relErr }

type harness struct {
	t        *testing.T
	file     string
	gate     *Gate
	lancet   *fakeLancet
	ctl      *Controller
	notices  []string
	levels   []string
	announce int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, file: filepath.Join(t.TempDir(), SettingsFileName), lancet: &fakeLancet{}}
	h.gate = NewGate(Settings{Mode: Interactive}, h.lancet)
	h.ctl = &Controller{Gate: h.gate, Lancet: h.lancet, Settings: h.file}
	return h
}

func (h *harness) run(args string) string {
	h.t.Helper()
	before := len(h.notices)
	h.ctl.Handle(context.Background(), args, func(m, l string) { h.notices = append(h.notices, m); h.levels = append(h.levels, l) }, func() { h.announce++ })
	if len(h.notices) == before {
		h.t.Fatalf("%q produced no notice", args)
	}
	return h.notices[len(h.notices)-1]
}

func TestNoArgumentTogglesTheModeAndPersistsIt(t *testing.T) {
	h := newHarness(t)
	if got := h.run(""); got != "smart-approve-lancet: strict - lancet off" {
		t.Errorf("first toggle: %q", got)
	}
	if h.gate.Mode() != Strict || LoadSettings(h.file).Mode != Strict {
		t.Error("strict was not applied and saved")
	}
	if got := h.run(""); got != "smart-approve-lancet: interactive - lancet off" || h.gate.Mode() != Interactive {
		t.Errorf("second toggle: %q mode=%s", got, h.gate.Mode())
	}
	h.run("STRICT")
	if h.gate.Mode() != Strict || h.announce != 3 {
		t.Errorf("explicit mode: mode=%s announces=%d", h.gate.Mode(), h.announce)
	}
}

func TestUnknownOptionChangesNothing(t *testing.T) {
	h := newHarness(t)
	got := h.run("auto")
	want := `smart-approve-lancet: unknown option "auto"; ` + CommandHelp
	if got != want || h.levels[0] != "error" {
		t.Errorf("got %q (%s)", got, h.levels[0])
	}
	if _, err := os.Stat(h.file); err == nil || h.gate.Mode() != Interactive || h.announce != 0 {
		t.Error("an unknown option changed state")
	}
}

func TestModeIsNotChangedWhenTheFileCannotBeSaved(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.file, []byte("[1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := h.run("strict")
	if !strings.Contains(got, "mode not changed; cannot save "+h.file) || h.gate.Mode() != Interactive || h.announce != 0 {
		t.Errorf("got %q mode=%s", got, h.gate.Mode())
	}
}

func TestStatusMentionsModeAndTheOmittedFeatures(t *testing.T) {
	h := newHarness(t)
	got := h.run("status")
	for _, want := range []string{"smart-approve-lancet: interactive - lancet off", "LLM risk analysis and auto mode are not part of this port", "settings: " + h.file} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
}

func TestLancetStatusReportsModelAndRuntime(t *testing.T) {
	h := newHarness(t)
	h.lancet.model = ModelInfo{Problem: "not downloaded", RuntimeProblem: "the ONNX Runtime library is not installed"}
	got := h.run("lancet status")
	for _, want := range []string{"LANCET: OFF", "model: not downloaded; run /smart-approve-lancet lancet setup", "runtime: unavailable (the ONNX Runtime library is not installed)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	h.lancet.model = ModelInfo{Directory: "/m", Installed: true, Verified: true, Runtime: "/lib/ort.so"}
	if got := h.run("lancet status"); !strings.Contains(got, "model: verified at /m (loads on the first bash command)") || !strings.Contains(got, "runtime: /lib/ort.so") {
		t.Errorf("verified status:\n%s", got)
	}
	h.lancet.model = ModelInfo{Installed: true}
	if got := h.run("lancet status"); !strings.Contains(got, "damaged (checksum mismatch)") {
		t.Errorf("damaged status:\n%s", got)
	}
}

func TestLancetOnNeedsAVerifiedModelAndAWorkingScorer(t *testing.T) {
	h := newHarness(t)
	got := h.run("lancet on")
	if !strings.Contains(got, "cannot enable: the pinned model is not verified") || h.gate.LancetOn() {
		t.Errorf("unverified: %q", got)
	}
	if _, err := os.Stat(h.file); err == nil {
		t.Error("settings were written for a refused enable")
	}

	h.lancet.model = ModelInfo{Installed: true, Verified: true}
	h.lancet.err = errors.New("library missing")
	got = h.run("lancet on")
	if !strings.Contains(got, "cannot enable: scoring does not work (library missing). Bash stays as it was.") || h.gate.LancetOn() {
		t.Errorf("scoring broken: %q", got)
	}

	h.lancet.err = nil
	h.lancet.result = verdict(lancet.NotFlagged, 0.1, "")
	got = h.run("lancet on")
	if !strings.HasPrefix(got, "LANCET: on.") || !h.gate.LancetOn() || !LoadSettings(h.file).Lancet {
		t.Errorf("enable: %q on=%v", got, h.gate.LancetOn())
	}
	if !slices.Contains(h.lancet.calls, "echo lancet-self-test") {
		t.Errorf("no self test: %q", h.lancet.calls)
	}
}

func TestLancetOffSavesReleasesAndReportsReleaseFailure(t *testing.T) {
	h := newHarness(t)
	h.gate.SetLancet(true)
	h.lancet.relErr = errors.New("busy")
	got := h.run("lancet off")
	if got != "LANCET: off. Bash uses the pattern checks only." || h.gate.LancetOn() || h.lancet.released != 1 {
		t.Errorf("got %q on=%v released=%d", got, h.gate.LancetOn(), h.lancet.released)
	}
	if !slices.Contains(h.notices, "LANCET is off, but releasing the runtime failed: busy") {
		t.Errorf("release failure not reported: %q", h.notices)
	}
	if LoadSettings(h.file).Lancet {
		t.Error("lancet.enabled still true in the file")
	}
}

func TestLancetOffWithAnUnsavableFileStaysOn(t *testing.T) {
	h := newHarness(t)
	h.gate.SetLancet(true)
	if err := os.WriteFile(h.file, []byte("[1]"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := h.run("lancet off")
	if !strings.Contains(got, "not changed; cannot save") || !h.gate.LancetOn() || h.lancet.released != 0 {
		t.Errorf("got %q", got)
	}
}

func TestLancetCheckScoresWithoutExecutingAndTruncates(t *testing.T) {
	h := newHarness(t)
	h.lancet.result = verdict(lancet.Review, 0.5, "uncertainty-band")
	got := h.run("lancet check echo hello")
	if got != "LANCET: review, score=0.5000, reason=uncertainty-band; command=echo hello (not executed)" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", 130)
	got = h.run("lancet check " + long)
	if !strings.Contains(got, "command="+strings.Repeat("x", 117)+"... (not executed)") {
		t.Errorf("not truncated: %q", got)
	}
	if got := h.run("lancet check"); got != "usage: /smart-approve-lancet lancet check <command>" {
		t.Errorf("empty check: %q", got)
	}
	h.lancet.err = errors.New("down")
	if got := h.run("lancet check ls"); got != "LANCET: check unavailable: down" {
		t.Errorf("unavailable: %q", got)
	}
}

func TestLancetSetupMessagesAndHelp(t *testing.T) {
	h := newHarness(t)
	h.lancet.setupOut = []string{"model", "ONNX Runtime"}
	if got := h.run("lancet setup"); !strings.Contains(got, "model and ONNX Runtime downloaded, verified and installed") {
		t.Errorf("downloaded: %q", got)
	}
	h.lancet.setupOut = nil
	if got := h.run("lancet setup"); !strings.Contains(got, "already installed and verified") {
		t.Errorf("current: %q", got)
	}
	h.lancet.setupErr = errors.New("HTTP 503")
	if got := h.run("lancet setup"); got != "LANCET: setup failed: HTTP 503" {
		t.Errorf("failure: %q", got)
	}
	if got := h.run("lancet frob"); got != LancetHelp {
		t.Errorf("help: %q", got)
	}
}

func TestCompletions(t *testing.T) {
	values := func(prefix string) []string {
		var out []string
		for _, c := range Completions(prefix) {
			out = append(out, c.Value)
		}
		return out
	}
	if got := values(""); !slices.Equal(got, []string{"interactive", "strict", "status", "lancet"}) {
		t.Errorf("empty: %v", got)
	}
	if got := values("st"); !slices.Equal(got, []string{"strict", "status"}) {
		t.Errorf("st: %v", got)
	}
	if got := values("lancet "); !slices.Equal(got, []string{"lancet status", "lancet setup", "lancet on", "lancet off", "lancet check"}) {
		t.Errorf("lancet: %v", got)
	}
	if got := values("lancet s"); !slices.Equal(got, []string{"lancet status", "lancet setup"}) {
		t.Errorf("lancet s: %v", got)
	}
	if got := values("lancet on "); len(got) != 0 {
		t.Errorf("third word: %v", got)
	}
}

func TestChip(t *testing.T) {
	h := newHarness(t)
	if got := h.ctl.Chip(); got != "smart-approve-lancet interactive - lancet off" {
		t.Errorf("chip = %q", got)
	}
}
