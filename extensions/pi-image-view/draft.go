package imageviewext

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

const maxDraftImages = 16

type draftImage struct {
	Label    string
	Original images.Image
	Preview  images.Image
}

type draftEditor interface {
	GetEditorText() (string, error)
	CompareAndSetEditorText(expected, text string) (bool, error)
}

type draftState struct {
	entries  map[string]draftImage
	lastText string
	// The editor owner clears text before emitting input. Keep one marker-keyed
	// snapshot until input consumes it or a new nonempty draft supersedes it.
	clearedEntries map[string]draftImage
}

func (d draftState) attachments(text string) []draftImage {
	var result []draftImage
	seen := make(map[string]bool)
	for _, label := range images.Markers.FindAllString(text, -1) {
		if value, ok := d.entries[label]; ok && !seen[label] {
			result = append(result, value)
			seen[label] = true
		}
	}
	return result
}

func (d draftState) submissionAttachments(text string) []draftImage {
	// The submitted description can be newer than the last poll. Resolve only
	// markers actually present in the submission, in their submitted order.
	if len(d.entries) == 0 {
		d.entries = d.clearedEntries
	}
	return d.attachments(text)
}

// scan commits attachment state only after the owner accepts the matching text.
// Unchanged drafts are skipped, including failed paths, to avoid repeated IO/notices.
func (d *draftState) scan(editor draftEditor, cwd string, allocate func(string) string, stop <-chan struct{}) (bool, []string, error) {
	text, err := editor.GetEditorText()
	if err != nil {
		return false, nil, fmt.Errorf("read image draft: %w", err)
	}
	if text == d.lastText {
		return false, nil, nil
	}
	candidate := make(map[string]draftImage)
	for _, entry := range d.attachments(text) {
		candidate[entry.Label] = entry
	}
	rendered := text
	var notices []string
	detected := images.Paths(text, cwd)
	trimmed := strings.TrimSpace(text)
	command := strings.HasPrefix(trimmed, "/") && (len(detected) == 0 || !strings.HasPrefix(trimmed, detected[0].Raw))
	if strings.HasPrefix(trimmed, "!") || command {
		detected = nil
	}
	for _, path := range detected {
		select {
		case <-stop:
			return false, nil, context.Canceled
		default:
		}
		if len(candidate) >= maxDraftImages {
			notices = append(notices, "At most 16 draft images; remaining paths were left unchanged")
			break
		}
		value, err := images.Read(path.File)
		var preview images.Image
		if err == nil {
			preview, err = images.Resize(value, true)
		}
		if err != nil {
			notices = append(notices, fmt.Sprintf("preview image %q: %v", path.File, err))
			continue
		}
		label := allocate(rendered)
		candidate[label] = draftImage{Label: label, Original: value, Preview: preview}
		rendered = strings.Replace(rendered, path.Raw, label, 1)
	}
	select {
	case <-stop:
		return false, nil, context.Canceled
	default:
	}
	if rendered != text {
		applied, err := editor.CompareAndSetEditorText(text, rendered)
		if err != nil {
			return false, nil, fmt.Errorf("replace image draft: %w", err)
		}
		if !applied {
			return false, nil, nil
		}
	}
	previous := d.attachments(d.lastText)
	current := draftState{entries: candidate}.attachments(rendered)
	changed := rendered != text || len(previous) != len(current)
	if !changed {
		for index := range current {
			if previous[index].Label != current[index].Label {
				changed = true
				break
			}
		}
	}
	if rendered == "" {
		d.clearedEntries = d.entries
	} else {
		d.clearedEntries = nil
	}
	d.entries, d.lastText = candidate, rendered
	return changed, notices, nil
}

// draftPoller owns one bounded, serial scanner and drains it before a restart.
type draftPoller struct {
	mu     sync.Mutex
	stopCh chan struct{}
	done   chan struct{}
}

func (p *draftPoller) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *draftPoller) stopLocked() {
	if p.stopCh == nil {
		return
	}
	close(p.stopCh)
	<-p.done
	p.stopCh, p.done = nil, nil
}

func (p *draftPoller) start(lifetimeError func() error, scan func(<-chan struct{}) error, report func(error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	stop, done := make(chan struct{}), make(chan struct{})
	p.stopCh, p.done = stop, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Query retained-context validity now, rather than capturing the startup request's Done channel.
				if lifetimeError != nil && lifetimeError() != nil {
					return
				}
				if err := scan(stop); err != nil {
					select {
					case <-stop:
						return
					default:
					}
					if lifetimeError != nil && lifetimeError() != nil {
						return
					}
					report(err)
					return
				}
			}
		}
	}()
}
