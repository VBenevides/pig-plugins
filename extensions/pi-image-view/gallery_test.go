package imageviewext

import (
	"fmt"
	"testing"

	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

func TestDraftGalleryTracksReorderRemovalAndClear(t *testing.T) {
	path := draftPNG(t, "gallery.png")
	editor := &draftEditorFixture{text: path + " " + path}
	var draft draftState
	next := 0
	allocate := func(string) string { next++; return fmt.Sprintf("[Image #%d]", next) }
	stop := make(chan struct{})
	if changed, notices, err := draft.scan(editor, "", allocate, stop); !changed || len(notices) != 0 || err != nil {
		t.Fatalf("changed=%v notices=%v err=%v", changed, notices, err)
	}
	if len(draft.attachments(editor.text)) != 2 {
		t.Fatal("latest preview replaced its neighbor")
	}
	decoded, err := images.Decode(draft.attachments(editor.text)[0].Preview)
	if err != nil || decoded.Bounds().Dx() != 960 {
		t.Fatalf("draft preview lost source detail: %v", err)
	}
	for _, text := range []string{"[Image #2] [Image #1]", "[Image #1]", ""} {
		editor.text = text
		if changed, _, err := draft.scan(editor, "", allocate, stop); !changed || err != nil {
			t.Fatalf("text=%q changed=%v err=%v", text, changed, err)
		}
		entries := draft.attachments(text)
		if text == "" && len(entries) != 0 {
			t.Fatal("cleared draft retained previews")
		}
		if text != "" && entries[0].Label != text[:10] {
			t.Fatalf("gallery order=%v", entries)
		}
	}
	remaining := draft.submissionAttachments("[Image #2] [Image #1]")
	if len(remaining) != 1 || remaining[0].Label != "[Image #1]" {
		t.Fatal("removed marker reappeared or the remaining attachment was lost")
	}
}

func TestClearedDraftSubmissionSnapshotMatchesAndExpires(t *testing.T) {
	text := "\ufeff\tinspect [Image #1] [Image #2]\n "
	draft := draftState{lastText: text, entries: map[string]draftImage{
		"[Image #1]": {Label: "[Image #1]"},
		"[Image #2]": {Label: "[Image #2]"},
	}}
	editor := &draftEditorFixture{}
	if changed, _, err := draft.scan(editor, "", draftLabels(), nil); !changed || err != nil {
		t.Fatalf("clear: changed=%v err=%v", changed, err)
	}
	if len(draft.attachments(text)) != 0 {
		t.Fatal("cleared images still appear in the draft gallery")
	}
	for _, fixture := range []struct {
		text   string
		labels []string
	}{
		{text, []string{"[Image #1]", "[Image #2]"}},
		{"inspect [Image #1] [Image #2]", []string{"[Image #1]", "[Image #2]"}},
		{"[Image #1][Image #2]Explain these images", []string{"[Image #1]", "[Image #2]"}},
		{"[Image #2][Image #1]new description", []string{"[Image #2]", "[Image #1]"}},
		{"different [Image #1]", []string{"[Image #1]"}},
		{"[Image #1] repeated [Image #1]", []string{"[Image #1]"}},
		{"different description with no markers", nil},
		{"unknown [Image #3]", nil},
	} {
		entries := draft.submissionAttachments(fixture.text)
		if len(entries) != len(fixture.labels) {
			t.Fatalf("submission %q has %d attachments, want %d", fixture.text, len(entries), len(fixture.labels))
		}
		for index, label := range fixture.labels {
			if entries[index].Label != label {
				t.Fatalf("submission %q attachment %d=%s, want %s", fixture.text, index, entries[index].Label, label)
			}
		}
	}
	if _, _, err := draft.scan(editor, "", draftLabels(), nil); err != nil {
		t.Fatal(err)
	}
	if len(draft.submissionAttachments(text)) != 2 {
		t.Fatal("unchanged empty editor dropped the delayed submission")
	}
	editor.text = "new draft"
	if _, _, err := draft.scan(editor, "", draftLabels(), nil); err != nil {
		t.Fatal(err)
	}
	if len(draft.submissionAttachments(text)) != 0 {
		t.Fatal("new draft did not retire the cleared snapshot")
	}
}
