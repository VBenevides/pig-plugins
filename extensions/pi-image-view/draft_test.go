package imageviewext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

type draftEditorFixture struct {
	text              string
	replacementTyping string
	failure           error
	writes            int
}

func (e *draftEditorFixture) GetEditorText() (string, error) { return e.text, nil }
func (e *draftEditorFixture) CompareAndSetEditorText(expected, text string) (bool, error) {
	if e.failure != nil {
		return false, e.failure
	}
	if e.replacementTyping != "" {
		e.text = e.replacementTyping
	}
	if expected != e.text {
		return false, nil
	}
	e.text = text
	e.writes++
	return true, nil
}

func draftPNG(t *testing.T, name string) string {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 960, 2))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func draftLabels() func(string) string {
	next := 1
	return func(text string) string {
		for strings.Contains(text, fmt.Sprintf("[Image #%d]", next)) {
			next++
		}
		label := fmt.Sprintf("[Image #%d]", next)
		next++
		return label
	}
}

func TestDraftPasteAutomaticallyReplacesPath(t *testing.T) {
	path := draftPNG(t, "pasted image.png")
	editor := &draftEditorFixture{text: "look at \"" + path + "\""}
	var draft draftState
	labels := draftLabels()
	changed, notices, err := draft.scan(editor, "", labels, nil)
	if err != nil || !changed || len(notices) != 0 {
		t.Fatalf("changed=%v notices=%v err=%v", changed, notices, err)
	}
	if editor.text != "look at [Image #1]" {
		t.Fatalf("draft=%q", editor.text)
	}
	entries := draft.attachments(editor.text)
	if len(entries) != 1 {
		t.Fatalf("attachments=%d", len(entries))
	}
	preview, err := images.Decode(entries[0].Preview)
	if err != nil || preview.Bounds().Dx() != 960 {
		t.Fatalf("preview=%v err=%v", preview, err)
	}
	original, err := images.Decode(entries[0].Original)
	if err != nil || original.Bounds().Dx() != 960 {
		t.Fatalf("original=%v err=%v", original, err)
	}
	// Temp-file lifetime no longer controls a successfully tracked attachment.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	changed, notices, err = draft.scan(editor, "", labels, nil)
	if err != nil || changed || len(notices) != 0 || editor.writes != 1 {
		t.Fatalf("repeated scan changed draft: %v %v %v", changed, notices, err)
	}
	if got := len(draft.attachments(editor.text + " [Image #1]")); got != 1 {
		t.Fatalf("duplicate marker duplicated attachment: %d", got)
	}
	editor.text = "look at"
	changed, _, err = draft.scan(editor, "", labels, nil)
	if err != nil || !changed || len(draft.entries) != 0 {
		t.Fatalf("deleted marker retained image: %v %v", changed, err)
	}
}

func TestDraftFailedPathKeepsSuccessfulNeighbor(t *testing.T) {
	path := draftPNG(t, "paste.png")
	missing := filepath.Join(filepath.Dir(path), "missing.png")
	editor := &draftEditorFixture{text: missing + " and " + path}
	var draft draftState
	labels := draftLabels()
	changed, notices, err := draft.scan(editor, "", labels, nil)
	if err != nil || !changed || len(notices) != 1 || !strings.Contains(notices[0], missing) {
		t.Fatalf("%v %v %v", changed, notices, err)
	}
	if editor.text != missing+" and [Image #1]" || len(draft.entries) != 1 {
		t.Fatalf("draft=%q entries=%d", editor.text, len(draft.entries))
	}
	_, notices, err = draft.scan(editor, "", labels, nil)
	if err != nil || len(notices) != 0 {
		t.Fatalf("unchanged failed path retried: %v %v", notices, err)
	}
}

func TestDraftRejectsStaleOrFailedReplacement(t *testing.T) {
	path := draftPNG(t, "paste.png")
	for _, failure := range []error{nil, errors.New("owner unavailable")} {
		editor := &draftEditorFixture{text: path, replacementTyping: "new typing", failure: failure}
		var draft draftState
		changed, _, err := draft.scan(editor, "", draftLabels(), nil)
		if changed || len(draft.entries) != 0 || draft.lastText != "" {
			t.Fatal("unaccepted draft committed attachment state")
		}
		if failure != nil && !errors.Is(err, failure) {
			t.Fatalf("lost original failure: %v", err)
		}
		if failure == nil && (err != nil || editor.text != "new typing") {
			t.Fatalf("overwrote typing: %q %v", editor.text, err)
		}
	}
}

func TestDraftCancellationAndShellInput(t *testing.T) {
	path := draftPNG(t, "paste.png")
	stop := make(chan struct{})
	close(stop)
	editor := &draftEditorFixture{text: path}
	var draft draftState
	_, _, err := draft.scan(editor, "", draftLabels(), stop)
	if !errors.Is(err, context.Canceled) || editor.writes != 0 {
		t.Fatalf("cancelled scan mutated draft: %v", err)
	}
	for _, prefix := range []string{"!cat ", "/pi-image-view preview ", " /read "} {
		editor.text = prefix + path
		changed, notices, err := draft.scan(editor, "", draftLabels(), nil)
		if err != nil || changed || len(notices) != 0 || editor.writes != 0 {
			t.Fatalf("command image path was intercepted: %q", editor.text)
		}
	}
}

func TestDraftAttachmentLimit(t *testing.T) {
	path := draftPNG(t, "paste.png")
	editor := &draftEditorFixture{text: strings.Repeat(path+" ", maxDraftImages+1)}
	var draft draftState
	_, notices, err := draft.scan(editor, "", draftLabels(), nil)
	if err != nil || len(notices) != 1 || len(draft.entries) != maxDraftImages || !strings.Contains(editor.text, path) {
		t.Fatalf("entries=%d notices=%v err=%v", len(draft.entries), notices, err)
	}
}

func TestDraftPollingStopsAndRestartsWithoutDuplicateWorkers(t *testing.T) {
	var polling draftPoller
	var active atomic.Int32
	started := make(chan struct{}, 2)
	scan := func(stop <-chan struct{}) error {
		if active.Add(1) != 1 {
			t.Error("overlapping scanners")
		}
		defer active.Add(-1)
		started <- struct{}{}
		<-stop
		return context.Canceled
	}
	fail := func(err error) { t.Errorf("cancelled scanner reported error: %v", err) }
	polling.start(nil, scan, fail)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not start")
	}
	polling.start(nil, scan, fail)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not restart")
	}
	polling.stop()
	polling.stop()
	if active.Load() != 0 {
		t.Fatal("stop returned with a live scanner")
	}
}
