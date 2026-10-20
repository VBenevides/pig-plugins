package eventprompt

import (
	"errors"
	"strings"
	"testing"
)

type lazyPrompt struct {
	text  string
	err   error
	calls int
}

func (p *lazyPrompt) Get() (string, error) { p.calls++; return p.text, p.err }

func TestReadLazyPromptAndPreserveCause(t *testing.T) {
	cause := errors.New("prompt resolution failed")
	for _, failure := range []error{nil, cause} {
		prompt := &lazyPrompt{text: "host guidance\nprior extension guidance", err: failure}
		text, err := Read("fixture", map[string]any{"systemPrompt": prompt})
		if !errors.Is(err, failure) || prompt.calls != 1 {
			t.Fatalf("err=%v calls=%d", err, prompt.calls)
		}
		if failure == nil && text != prompt.text {
			t.Fatal(text)
		}
		if failure != nil && !strings.Contains(err.Error(), "fixture") {
			t.Fatal(err)
		}
	}
}

func TestReadLegacyAndMalformedEvents(t *testing.T) {
	text, err := Read("fixture", map[string]any{"systemPrompt": "base"})
	if err != nil || text != "base" {
		t.Fatal(text, err)
	}
	for _, data := range []map[string]any{nil, {}, {"systemPrompt": nil}, {"systemPrompt": 42}} {
		if _, err := Read("fixture", data); err == nil {
			t.Fatal("accepted malformed prompt", data)
		}
	}
}
