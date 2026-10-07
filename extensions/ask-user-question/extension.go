// Package askuserquestion ports the structured rpiv questionnaire to PiG's remote UI.
package askuserquestion

import (
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/ask"
	"log"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const Name = "ask-user-question"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	config := ask.LoadConfig(os.Getenv)
	if config.Warning != "" {
		log.Print(config.Warning)
	}
	e.RegisterTool(sdk.ToolDefinition{Name: "ask_user_question", Label: "Ask User Question", Exposure: sdk.ToolExposureModelOnly, Description: config.Guidance.Description, PromptSnippet: config.Guidance.Snippet, PromptGuidelines: config.Guidance.Guidelines, Parameters: ask.Schema(), Execute: execute})
	return e
}

type component struct {
	state *ask.State
	theme sdk.UITheme
}

func (c *component) Render(width int) []string {
	lines := c.state.Render(width)
	for i, line := range lines {
		if strings.HasPrefix(line, "+") {
			lines[i] = c.theme.Fg("borderAccent", line)
		} else if strings.HasPrefix(line, "| >") {
			lines[i] = c.theme.Fg("accent", line)
		}
	}
	return lines
}
func (c *component) HandleInput(data string) (sdk.RemoteComponentResult, error) {
	result, err := c.state.Handle(data)
	if c.state.EditorRequested {
		c.state.EditorRequested = false
		// Close this custom UI before opening another host dialog.
		return sdk.RemoteComponentResult{Done: true, Value: map[string]any{"editorRequest": true}}, err
	}
	if result == nil {
		return sdk.RemoteComponentResult{}, err
	}
	return sdk.RemoteComponentResult{Done: true, Value: *result}, err
}

const PromptEvent = "rpiv:ask-user:prompt"
const BlockedEvent = "rpiv:ask-user:blocked"

func prompt(p ask.Params) any {
	questions := make([]map[string]any, len(p.Questions))
	for i, q := range p.Questions {
		options := make([]map[string]any, len(q.Options))
		for j, o := range q.Options {
			options[j] = map[string]any{"label": o.Label, "description": o.Description, "hasPreview": o.Preview != ""}
		}
		questions[i] = map[string]any{"question": q.Question, "header": q.Header, "multiSelect": q.Multi, "options": options}
	}
	return map[string]any{"questions": questions}
}
func execute(ctx sdk.Context, raw map[string]any) (out any, retErr error) {
	if !ctx.HasUI() || (ctx.Mode() != "tui" && ctx.Mode() != "rpc") {
		return sdk.ToolResult{Content: "Error: UI not available (running in non-interactive mode)", Details: ask.Empty("no_ui"), IsError: true}, nil
	}
	p, err := ask.Parse(raw)
	if err != nil {
		var validation *ask.ValidationError
		if errors.As(err, &validation) {
			return sdk.ToolResult{Content: "Error: " + validation.Message, Details: ask.Empty(validation.Code), IsError: true}, nil
		}
		return nil, err
	}
	if err := ctx.Events().Emit(PromptEvent, prompt(p)); err != nil {
		return nil, fmt.Errorf("publish questionnaire prompt: %w", err)
	}
	if err := ctx.Events().Emit(BlockedEvent, map[string]any{"active": true}); err != nil {
		return nil, fmt.Errorf("publish questionnaire wait: %w", err)
	}
	defer func() {
		if err := ctx.Events().Emit(BlockedEvent, map[string]any{"active": false}); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("clear questionnaire wait: %w", err))
		}
	}()
	var result ask.Result
	if ctx.Mode() == "rpc" {
		result, err = runRPC(ctx, p)
	} else {
		config := ask.LoadConfig(os.Getenv)
		if config.Warning != "" {
			log.Print(config.Warning)
		}
		state := ask.New(p)
		state.CollapseKey = config.CollapseKey
		view := &component{state: state, theme: ctx.UITheme()}
		options := sdk.RemoteOverlayOptions{Title: "Questions", Overlay: true, OverlayOptions: &sdk.OverlayOptions{Anchor: "bottom-center", Width: sdk.OverlayPercent(100), MaxHeight: sdk.OverlayPercent(100)}}
		for {
			value, callErr := ctx.Custom(view, options)
			if callErr != nil {
				err = callErr
				break
			}
			if value == nil {
				err = errors.New("custom UI returned no questionnaire result")
				break
			}
			data, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				err = marshalErr
				break
			}
			var response struct {
				ask.Result
				EditorRequest bool `json:"editorRequest"`
			}
			if err = json.Unmarshal(data, &response); err != nil {
				break
			}
			if !response.EditorRequest {
				result = response.Result
				if result.Answers == nil {
					err = errors.New("custom UI returned invalid questionnaire details")
				}
				break
			}
			text, confirmed, editErr := ctx.Editor("Edit custom answer", state.CustomText())
			if editErr != nil {
				err = editErr
				break
			}
			if confirmed {
				state.ReplaceCustomText(text)
			}
		}
	}
	if err != nil {
		return sdk.ToolResult{Content: "Error: questionnaire UI failed: " + err.Error(), Details: ask.Empty("no_custom_ui"), IsError: true}, nil
	}
	text, result := ask.Envelope(result, p)
	return sdk.ToolResult{Content: text, Details: result}, nil
}

var leadingIndex = regexp.MustCompile(`^\s*([+-]?\d+)`)
var multiIndex = regexp.MustCompile(`^\d+\.?$`)

func parseIndex(value string, count int) int {
	match := leadingIndex.FindStringSubmatch(value)
	if len(match) < 2 {
		return -1
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n < 1 || n > count {
		return -1
	}
	return n - 1
}
func runRPC(ctx sdk.Context, p ask.Params) (ask.Result, error) {
	r := ask.Result{Answers: []ask.Answer{}}
	for i, q := range p.Questions {
		header := ""
		if q.Header != "" {
			header = "[" + q.Header + "] "
		}
		title := header + q.Question
		options := make([]string, len(q.Options))
		for j, o := range q.Options {
			options[j] = fmt.Sprintf("%d. %s — %s", j+1, o.Label, o.Description)
		}
		a := ask.Answer{Index: i, Question: q.Question}
		if q.Multi {
			value, ok, err := ctx.Input(title+"\n\n"+strings.Join(options, "\n")+"\n\nEnter the numbers of all that apply, comma-separated (e.g. \"1,3\"), or type a custom answer as plain text.", "1,3")
			if err != nil {
				return r, err
			}
			if !ok {
				r.Cancelled = true
				return r, nil
			}
			value = strings.TrimSpace(value)
			selected := []string{}
			valid := true
			for _, token := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
				index := parseIndex(token, len(q.Options))
				if !multiIndex.MatchString(token) || index < 0 {
					valid = false
					break
				}
				label := q.Options[index].Label
				if !slices.Contains(selected, label) {
					selected = append(selected, label)
				}
			}
			if valid {
				a.Kind = "multi"
				a.Selected = new(selected)
			} else {
				a.Kind = "custom"
				a.Answer = new(value)
			}
		} else {
			for j, o := range q.Options {
				if o.Preview != "" {
					preview := []rune(o.Preview)
					if len(preview) > 600 {
						preview = preview[:600]
					}
					title += fmt.Sprintf("\n\n--- %d. %s preview ---\n%s", j+1, o.Label, string(preview))
				}
			}
			options = append(options, fmt.Sprintf("%d. Type something.", len(options)+1))
			chosen, ok, err := ctx.Select(title, options)
			if err != nil {
				return r, err
			}
			index := parseIndex(chosen, len(options))
			if !ok || index < 0 {
				r.Cancelled = true
				return r, nil
			}
			if index < len(q.Options) {
				o := q.Options[index]
				a.Kind = "option"
				a.Answer = new(o.Label)
				a.Preview = o.Preview
			} else {
				value, ok, err := ctx.Input(header+q.Question+"\n\nType your answer:", "")
				if err != nil {
					return r, err
				}
				if !ok {
					r.Cancelled = true
					return r, nil
				}
				a.Kind = "custom"
				a.Answer = new(value)
			}
		}
		r.Answers = append(r.Answers, a)
	}
	return r, nil
}
