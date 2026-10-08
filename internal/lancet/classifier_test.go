package lancet

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The real model and the ONNX Runtime library are large, separately downloaded files. Point at them with
// LANCET_MODEL_DIR and LANCET_ORT_LIBRARY.
func realModel(t *testing.T) (dir, library string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir = os.Getenv("LANCET_MODEL_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".pig", "agent", "smart-approve-lancet", ModelID)
	}
	library = os.Getenv("LANCET_ORT_LIBRARY")
	if _, err := os.Stat(filepath.Join(dir, "encoder-int8.onnx")); err != nil {
		t.Skipf("LANCET model not installed in %s (set LANCET_MODEL_DIR)", dir)
	}
	if library == "" {
		t.Skip("ONNX Runtime library not found (set LANCET_ORT_LIBRARY)")
	}
	return dir, library
}

func loadReal(t *testing.T) *Classifier {
	t.Helper()
	dir, library := realModel(t)
	c, err := Load(dir, library)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Scores and bands must equal the official v0.4.3 CPU results (the fixture is the one smart-approve-lancet
// checks), including inputs that span several 512-token windows.
func TestScoresMatchOfficialV043(t *testing.T) {
	c := loadReal(t)
	var fixture struct {
		Cases []struct {
			Command        string  `json:"command"`
			Score          float64 `json:"score"`
			Classification string  `json:"classification"`
			Reason         *string `json:"reason"`
			RiskLogit      float64 `json:"riskLogit"`
		} `json:"cases"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "lancet-v043-parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, row := range fixture.Cases {
		name := row.Command
		if len(name) > 40 {
			name = name[:40]
		}
		t.Run(name, func(t *testing.T) {
			got, err := c.Score(context.Background(), row.Command, "bash")
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Classification) != row.Classification {
				t.Fatalf("classification = %s, want %s (logit %v, want %v)", got.Classification, row.Classification, got.Logit, row.RiskLogit)
			}
			if got.Score == nil || math.Abs(*got.Score-row.Score) >= 1e-6 {
				t.Fatalf("score = %v, want %v", got.Score, row.Score)
			}
			wantReason := ""
			if row.Reason != nil {
				wantReason = *row.Reason
			}
			if got.Reason != wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, wantReason)
			}
		})
	}
}

func TestRefusedInputIsReviewWithoutScore(t *testing.T) {
	c := loadReal(t)
	cases := []struct{ name, command, shell, reason string }{
		{"unsupported shell", "git status", "powershell", "unsupported-shell"},
		{"blank", " ", "bash", "empty-command"},
		{"python-only whitespace", "\x1c\x1f", "bash", "empty-command"},
		{"nul byte", "echo \x00", "bash", "nul-byte"},
		{"invalid utf-8", "echo \xff", "bash", "invalid-unicode"},
		{"too long", strings.Repeat("x", 8193), "bash", "raw-input-too-long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Score(context.Background(), tc.command, tc.shell)
			if err != nil {
				t.Fatal(err)
			}
			if got.Classification != Review || got.Score != nil || got.Reason != tc.reason {
				t.Fatalf("got %+v, want review/no score/%s", got, tc.reason)
			}
		})
	}
}

func TestCancelledContextIsAnErrorNotAVerdict(t *testing.T) {
	c := loadReal(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Score(ctx, "git status", "bash"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	// Cancelling while a long, multi-window input is being scored must stop it promptly.
	long := strings.Repeat("echo hello world; ", 400)
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := c.Score(ctx, long, "bash")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancelled score took %v", elapsed)
	}
	// The classifier stays usable afterwards.
	got, err := c.Score(context.Background(), "git status", "bash")
	if err != nil || got.Classification != NotFlagged {
		t.Fatalf("after cancel: %+v, %v", got, err)
	}
}

func TestLoadRejectsTamperedArtifact(t *testing.T) {
	dir, library := realModel(t)
	copyDir := t.TempDir()
	for name := range modelFiles {
		if name == "encoder-int8.onnx" {
			continue // large; the checksum of a small file fails first
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "vocab.json" {
			data[len(data)/2] ^= 0x01 // same size, different content
		}
		if err := os.WriteFile(filepath.Join(copyDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// readVerified runs in the order model, vocab, ...: vocab.json fails its checksum before the encoder is read.
	_, err := Load(copyDir, library)
	if err == nil || !strings.Contains(err.Error(), "vocab.json") {
		t.Fatalf("err = %v, want a checksum or missing-file error naming vocab.json or the encoder", err)
	}
}

func TestPreSplitFollowsThePretokenizerPattern(t *testing.T) {
	// Each row is what the shipped pattern
	//   's|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+
	// yields. Only a plain space joins the following word; a whitespace run followed by a non-space gives its
	// last character back, so "a  b" is "a", " ", " b" but "ls\n\nrm" is "ls", "\n", "\n", "rm".
	cases := []struct {
		text string
		want []string
	}{
		{"git status", []string{"git", " status"}},
		{"a  b", []string{"a", " ", " b"}},
		{"a \tb", []string{"a", " ", "\t", "b"}},
		{"echo $HOME/x", []string{"echo", " $", "HOME", "/", "x"}},
		{"don't", []string{"don", "'t"}},
		{"I'll we're", []string{"I", "'ll", " we", "'re"}},
		{"x  ", []string{"x", "  "}},
		{"ls\n\nrm", []string{"ls", "\n", "\n", "rm"}},
		{"n1 22", []string{"n", "1", " 22"}},
		{"café 😀", []string{"café", " 😀"}},
	}
	for _, tc := range cases {
		if got := preSplit(tc.text); !slices.Equal(got, tc.want) {
			t.Errorf("preSplit(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
