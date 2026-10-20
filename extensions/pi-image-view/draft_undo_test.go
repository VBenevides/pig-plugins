package imageviewext

import (
	"fmt"
	"testing"
)

func TestDraftUndoRestoresDeletedImagePayloads(t *testing.T) {
	original := "inspect [Image #1] [Image #2]"
	draft := draftState{lastText: original, entries: map[string]draftImage{
		"[Image #1]": {Label: "[Image #1]"},
		"[Image #2]": {Label: "[Image #2]"},
	}}
	editor := &draftEditorFixture{}
	for _, replacement := range []string{"T", "selection paste", "", "inspect [Image #1]"} {
		editor.text = replacement
		if _, _, err := draft.scan(editor, "", draftLabels(), nil); err != nil {
			t.Fatal(err)
		}
		want := 0
		if replacement == "inspect [Image #1]" {
			want = 1
		}
		if got := len(draft.submissionAttachments(editor.text)); got != want {
			t.Fatalf("deleted markers leaked into submission: %d, want %d", got, want)
		}
		editor.text = original // The editor restores text from its own undo stack.
		changed, _, err := draft.scan(editor, "", draftLabels(), nil)
		if err != nil || !changed || len(draft.attachments(original)) != 2 {
			t.Fatalf("undo after %q: changed=%v attachments=%v err=%v", replacement, changed, draft.attachments(original), err)
		}
	}
}

func TestDraftUndoRetentionIsBoundedAndNewImagesWin(t *testing.T) {
	draft := draftState{entries: make(map[string]draftImage), lastText: "old draft"}
	for i := 1; i <= maxDraftImages; i++ {
		label := fmt.Sprintf("[Image #%d]", i)
		draft.entries[label] = draftImage{Label: label}
	}
	editor := &draftEditorFixture{text: draftPNG(t, "new.png")}
	if _, _, err := draft.scan(editor, "", func(string) string { return "[Image #17]" }, nil); err != nil {
		t.Fatal(err)
	}
	if len(draft.undoEntries) != maxDraftImages {
		t.Fatalf("retained payloads=%d, want %d", len(draft.undoEntries), maxDraftImages)
	}
	attachments := draft.submissionAttachments(editor.text)
	if len(attachments) != 1 || attachments[0].Label != "[Image #17]" {
		t.Fatalf("new image lost or inactive images sent: %v", attachments)
	}
}
