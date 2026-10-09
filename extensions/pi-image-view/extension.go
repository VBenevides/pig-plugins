// Package imageviewext implements pi-image-view without a JavaScript runtime.
package imageviewext

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/agentdir"
	images "github.com/VBenevides/pig-plugins/internal/imageview"
)

const Name = "pi-image-view"

func blobRoot() string {
	return filepath.Join(agentdir.Dir(os.Getenv), "image-view", "blobs")
}
func blocks(value any) []map[string]any {
	var items []map[string]any
	switch typed := value.(type) {
	case []map[string]any:
		items = typed
	case []any:
		for _, item := range typed {
			if block, ok := item.(map[string]any); ok {
				items = append(items, block)
			}
		}
	default:
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copy := make(map[string]any, len(item))
		for key, value := range item {
			copy[key] = value
		}
		result = append(result, copy)
	}
	return result
}
func asImage(block map[string]any) images.Image {
	data, _ := block["data"].(string)
	mime, _ := block["mimeType"].(string)
	return images.Image{Type: "image", Data: data, MimeType: mime}
}
func imageMap(value images.Image) map[string]any {
	return map[string]any{"type": "image", "data": value.Data, "mimeType": value.MimeType}
}
func textBlock(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

// Only visible text reserves numbers; assistant output and encoded image data do not.
func nextImageNumber(branch []map[string]any) int {
	next := 1
	for _, entry := range branch {
		message, _ := entry["message"].(map[string]any)
		role, _ := message["role"].(string)
		if role != "user" && role != "toolResult" {
			continue
		}
		texts := []string{}
		if text, ok := message["content"].(string); ok {
			texts = append(texts, text)
		} else {
			for _, block := range blocks(message["content"]) {
				if text, ok := block["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		for _, text := range texts {
			for _, match := range images.Markers.FindAllStringSubmatch(text, -1) {
				number, err := strconv.Atoi(match[1])
				if err == nil && number < 1_000_000_000 {
					next = max(next, number+1)
				}
			}
		}
	}
	return next
}

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var mu sync.Mutex
	var draft draftState
	var polling draftPoller
	next := 1
	detail := false
	clearBefore := -1
	clearNext := false
	lastCount := 0
	restoreState := func(ctx sdk.Context, _ map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		next = 1
		detail = false
		clearBefore = -1
		clearNext = false
		lastCount = 0
		draft = draftState{}
		branch, err := ctx.SessionManager().GetBranch(nil)
		if err != nil {
			return nil, fmt.Errorf("restore image numbering: %w", err)
		}
		next = nextImageNumber(branch)
		if ctx.HasUI() {
			return nil, ctx.SetWidget("image-view", nil)
		}
		return nil, nil
	}
	marker := func(text string) string {
		for strings.Contains(text, fmt.Sprintf("[Image #%d]", next)) {
			next++
		}
		label := fmt.Sprintf("[Image #%d]", next)
		next++
		return label
	}
	show := func(ctx sdk.Context, value images.Image, label string) {
		if err := showGallery(ctx, []draftImage{{Label: label, Preview: value}}, ctx.Width()); err != nil {
			ctx.Notify(err.Error(), "warn")
		}
	}
	restore := func(ctx sdk.Context, event map[string]any) (any, error) {
		polling.stop()
		if _, err := restoreState(ctx, event); err != nil {
			return nil, err
		}
		if ctx.HasUI() && ctx.Mode() == "tui" {
			editor, ok := any(ctx).(draftEditor)
			if !ok {
				return nil, fmt.Errorf("automatic image previews require the patched pig-plugins SDK; rebuild with scripts/dev_build.sh")
			}
			previewWidth := 0
			polling.start(ctx.Err, func(stop <-chan struct{}) error {
				mu.Lock()
				defer mu.Unlock()
				changed, notices, err := draft.scan(editor, ctx.Cwd(), marker, stop)
				if err != nil {
					return err
				}
				select {
				case <-stop:
					return nil
				default:
				}
				for _, notice := range notices {
					ctx.Notify(notice, "warn")
				}
				width := ctx.Width()
				if changed || previewWidth != width {
					if err := showGallery(ctx, draft.attachments(draft.lastText), width); err != nil {
						return err
					}
					previewWidth = width
				}
				return nil
			}, func(err error) { ctx.Notify(fmt.Sprintf("automatic image preview stopped: %v", err), "warn") })
		}
		return nil, nil
	}
	e.OnEvent(sdk.EventSessionStart, restore)
	e.OnEvent(sdk.EventSessionTree, restore)
	e.OnEvent(sdk.EventSessionShutdown, func(_ sdk.Context, _ map[string]any) (any, error) {
		polling.stop()
		return nil, nil
	})
	e.RegisterCommand(Name, sdk.CommandOptions{Description: "Arm detail mode, clear old model images, or preview an image path", Handler: func(ctx sdk.Context, args string) error {
		mu.Lock()
		defer mu.Unlock()
		switch strings.TrimSpace(args) {
		case "detail":
			detail = true
			ctx.Notify("Next image submission uses 1280px detail mode", "info")
			return nil
		case "clear":
			if lastCount > 0 {
				clearBefore = lastCount
			} else {
				clearNext = true
			}
			ctx.Notify("Existing images cleared from future model context; history preserved", "info")
			return nil
		}
		if strings.HasPrefix(args, "preview ") {
			path := strings.Trim(strings.TrimSpace(strings.TrimPrefix(args, "preview ")), "\"'")
			if !filepath.IsAbs(path) {
				path = filepath.Join(ctx.Cwd(), path)
			}
			value, err := images.Read(path)
			if err != nil {
				return err
			}
			prepared, err := images.Resize(value, false)
			if err != nil {
				return err
			}
			show(ctx, prepared, "Image preview")
			return nil
		}
		return fmt.Errorf("usage: /pi-image-view [detail|clear|preview PATH]")
	}})
	e.MarkdownTransformer(func(markdown string, context sdk.MarkdownTransformContext) string {
		if context.MessageType != "user" {
			return markdown
		}
		return images.Links.ReplaceAllStringFunc(markdown, func(link string) string {
			match := images.Links.FindStringSubmatch(link)
			ref := images.Reference.FindStringSubmatch(match[2])
			if ref == nil {
				return link
			}
			target := (&url.URL{Scheme: "file", Path: filepath.Join(blobRoot(), ref[1])}).String()
			return "[[Image #" + match[1] + "]](" + target + ")"
		})
	})
	e.OnEvent(sdk.EventInput, func(ctx sdk.Context, event map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		text, _ := event["text"].(string)
		detected := images.Paths(text, ctx.Cwd())
		if strings.HasPrefix(strings.TrimSpace(text), "!") || strings.HasPrefix(text, "/") && len(detected) == 0 {
			return map[string]any{"action": "continue"}, nil
		}
		existing := blocks(event["images"])
		tracked := draft.submissionAttachments(text)
		if len(existing)+len(detected)+len(tracked) > maxDraftImages {
			return nil, fmt.Errorf("at most 16 image attachments or paths per submission")
		}
		output := append([]map[string]any{}, existing...)
		matched := make([]bool, len(existing))
		preparedCount := 0
		attach := func(value images.Image, token string, index int, label string) error {
			prepared, err := images.Resize(value, detail)
			if err != nil {
				return err
			}
			ref, err := images.Store(prepared, blobRoot())
			if err != nil {
				return err
			}
			trackedMarker := label != ""
			if label == "" {
				label = marker(text)
			}
			link := "[" + label + "](" + ref + ")"
			if token != "" {
				if trackedMarker {
					text = strings.ReplaceAll(text, token, link)
				} else {
					text = strings.Replace(text, token, link, 1)
				}
			} else {
				text = strings.TrimSpace(text + " " + link)
			}
			if index >= 0 {
				output[index] = imageMap(prepared)
				matched[index] = true
			} else {
				output = append(output, imageMap(prepared))
			}
			preparedCount++
			return nil
		}
		matchingAttachment := func(value images.Image) int {
			for i, block := range existing {
				if !matched[i] && asImage(block).Data == value.Data {
					return i
				}
			}
			return -1
		}
		for _, entry := range tracked {
			if err := attach(entry.Original, entry.Label, matchingAttachment(entry.Original), entry.Label); err != nil {
				return nil, fmt.Errorf("prepare draft %s: %w", entry.Label, err)
			}
		}
		for _, path := range detected {
			value, err := images.Read(path.File)
			if err == nil {
				err = attach(value, path.Raw, matchingAttachment(value), "")
			}
			if err != nil {
				ctx.Notify(fmt.Sprintf("attach image %q: %v", path.File, err), "warn")
			}
		}
		for i, block := range existing {
			if matched[i] {
				continue
			}
			if err := attach(asImage(block), "", i, ""); err != nil {
				return nil, fmt.Errorf("prepare attachment %d: %w", i+1, err)
			}
		}
		if preparedCount == 0 {
			draft.clearedEntries = nil
			return map[string]any{"action": "continue"}, nil
		}
		if err := showGallery(ctx, nil, ctx.Width()); err != nil {
			ctx.Notify(fmt.Sprintf("clear submitted image previews: %v", err), "warn")
		}
		detail = false
		draft = draftState{}
		return map[string]any{"action": "transform", "text": text, "images": output}, nil
	})
	e.OnEvent(sdk.EventToolResult, func(ctx sdk.Context, event map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if failed, _ := event["isError"].(bool); failed {
			return nil, nil
		}
		name, _ := event["toolName"].(string)
		screenshot := strings.HasSuffix(name, "take_screenshot")
		if details, ok := event["details"].(map[string]any); ok {
			tool, _ := details["tool"].(string)
			screenshot = screenshot || strings.HasSuffix(tool, "take_screenshot")
		}
		if name != "read" && !screenshot {
			return nil, nil
		}
		content := blocks(event["content"])
		output := []map[string]any{}
		changed := false
		for _, block := range content {
			if block["type"] != "image" {
				output = append(output, block)
				continue
			}
			value, err := images.Resize(asImage(block), false)
			if err != nil {
				return nil, fmt.Errorf("prepare %s image: %w", name, err)
			}
			label := marker("")
			output = append(output, textBlock(label), imageMap(value))
			changed = true
			show(ctx, value, label)
		}
		if screenshot && !changed {
			for _, block := range content {
				text, _ := block["text"].(string)
				for _, line := range strings.Split(text, "\n") {
					if !strings.HasPrefix(line, "Saved screenshot to ") {
						continue
					}
					path := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "Saved screenshot to ")), ".")
					if !filepath.IsAbs(path) {
						path = filepath.Join(ctx.Cwd(), path)
					}
					value, err := images.Read(path)
					if err == nil {
						value, err = images.Resize(value, false)
					}
					if err != nil {
						ctx.Notify(fmt.Sprintf("load screenshot %q: %v", path, err), "warn")
						continue
					}
					label := marker("")
					output = append(output, textBlock(label), imageMap(value))
					show(ctx, value, label)
					changed = true
				}
			}
		}
		if changed {
			return map[string]any{"content": output}, nil
		}
		return nil, nil
	})
	e.OnEvent(sdk.EventContext, func(_ sdk.Context, event map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		messages := blocks(event["messages"])
		if clearBefore >= 0 && len(messages) < lastCount {
			clearBefore = max(0, clearBefore-(lastCount-len(messages)))
		}
		if clearNext {
			clearBefore = len(messages)
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i]["role"] == "user" {
					clearBefore = i
					break
				}
			}
			clearNext = false
		}
		lastCount = len(messages)
		for i, message := range messages {
			if text, ok := message["content"].(string); ok {
				message["content"] = images.Strip(text)
				continue
			}
			content := blocks(message["content"])
			if content == nil {
				continue
			}
			filtered := []map[string]any{}
			for _, block := range content {
				if block["type"] == "image" && i < clearBefore {
					continue
				}
				if text, ok := block["text"].(string); ok {
					block["text"] = images.Strip(text)
				}
				filtered = append(filtered, block)
			}
			if len(filtered) == 0 && len(content) > 0 {
				filtered = append(filtered, textBlock("[Image omitted from model context]"))
			}
			message["content"] = filtered
		}
		return map[string]any{"messages": messages}, nil
	})
	return e
}
