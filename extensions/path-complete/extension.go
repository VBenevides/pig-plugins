// Package pathcompleteext completes filesystem paths in the prompt. Type ~/, ./, ../ or / and a list of the
// matching files and folders shows up. /fs <path> browses a folder; the path may hold spaces.
package pathcompleteext

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/pathcomplete"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

// Name is the extension identity.
const Name = "path-complete"

const fsCommand = "fs"

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var registered atomic.Bool

	sdkctx.OnSessionStart(e, func(ctx sdk.Context, _ map[string]any) (any, error) {
		// The host keeps the provider for the life of the connection, so one registration is enough.
		if !registered.CompareAndSwap(false, true) {
			return nil, nil
		}
		if err := ctx.AddAutocompleteProvider(newProvider); err != nil {
			registered.Store(false)
			return nil, fmt.Errorf("register path autocomplete: %w", err)
		}
		return nil, nil
	})

	e.RegisterCommand(fsCommand, sdk.CommandOptions{
		Description: "Browse folders and put a path in the prompt: /fs ~/Projects/my folder (spaces need no quotes)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("current directory: %w", err)
			}
			return fsCompletions(cwd, prefix)
		},
		Handler: func(ctx sdk.Context, args string) error {
			path := pathcomplete.Decode(args)
			if path == "" {
				ctx.Notify("usage: /fs <path> (choose an entry to put the path in the prompt)", "warning")
				return nil
			}
			// The command is replaced by the path itself, quoted when it has spaces; nothing is sent.
			text, _ := pathcomplete.Apply("", 0, pathcomplete.Token{}, path)
			ctx.SetEditorText(text)
			return nil
		},
	})
	return e
}

const fsPrefix = "/" + fsCommand + " "

// findToken returns the path being typed at the cursor. In the arguments of /fs the whole rest of the line is
// the path (fs is true), so spaces need no quotes and the value is inserted as typed.
func findToken(lines []string, line, col int) (tok pathcomplete.Token, cursor int, fs, ok bool) {
	if line < 0 || line >= len(lines) {
		return tok, 0, false, false
	}
	cursor = pathcomplete.ByteOffset(lines[line], col)
	if line == 0 && strings.HasPrefix(lines[0], fsPrefix) && cursor >= len(fsPrefix) {
		raw := lines[0][len(fsPrefix):cursor]
		return pathcomplete.Token{Start: len(fsPrefix), Raw: raw, Path: pathcomplete.Decode(raw)}, cursor, true, true
	}
	tok, ok = pathcomplete.Find(lines[line], cursor)
	return tok, cursor, false, ok
}

func resolveDir(cwd, dir string) (string, error) {
	home := ""
	if strings.HasPrefix(dir, "~/") {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("resolve ~ in %q: %w", dir, err)
		}
	}
	return pathcomplete.Resolve(home, cwd, dir), nil
}

// pathTriggers open the menu on their own when typed at the start of a word. The editor ignores "/" as a
// trigger, so ~/ and ./ and ../ start with their first character, and "\"" starts a quoted path. A leading "/"
// at the start of the prompt opens the menu as a slash command, and the Tab key opens it anywhere.
var pathTriggers = []string{"~", ".", `"`}

// newProvider wraps current: paths are ours, everything else stays with the wrapped provider.
func newProvider(_ sdk.Context, current *sdk.AutocompleteProvider) (*sdk.AutocompleteProvider, error) {
	triggers := slices.Concat(current.TriggerCharacters, pathTriggers)
	slices.Sort(triggers)
	p := &sdk.AutocompleteProvider{TriggerCharacters: slices.Compact(triggers)}
	p.GetSuggestions = func(ctx sdk.Context, lines []string, line, col int, force bool) (*sdk.AutocompleteSuggestions, error) {
		tok, _, _, ok := findToken(lines, line, col)
		if !ok {
			return current.GetSuggestions(ctx, lines, line, col, force)
		}
		dir, prefix := pathcomplete.Split(tok.Path)
		abs, err := resolveDir(ctx.Cwd(), dir)
		if err != nil {
			return nil, err
		}
		entries, more, err := pathcomplete.List(abs, prefix)
		if err != nil || len(entries) == 0 {
			return nil, err
		}
		items := autocompleteItems(pathcomplete.Choices(dir, prefix, entries))
		if len(items) == 0 {
			return nil, nil
		}
		if more > 0 {
			items[len(items)-1].Description = fmt.Sprintf("+%d more, keep typing to narrow", more)
		}
		return &sdk.AutocompleteSuggestions{Items: items, Prefix: tok.Raw}, nil
	}
	p.ApplyCompletion = func(ctx sdk.Context, lines []string, line, col int, item sdk.AutocompleteItem, prefix string) (sdk.AutocompleteCompletion, error) {
		tok, cursor, fs, ok := findToken(lines, line, col)
		if !ok || tok.Raw != prefix {
			return current.ApplyCompletion(ctx, lines, line, col, item, prefix)
		}
		if fs {
			// Choosing a path drops "/fs " from the prompt; a folder keeps completing inline.
			tok.Start = 0
		}
		text, end := pathcomplete.Apply(lines[line], cursor, tok, item.Value)
		out := slices.Clone(lines)
		out[line] = text
		return sdk.AutocompleteCompletion{Lines: out, CursorLine: line, CursorCol: pathcomplete.UTF16Len(text[:end])}, nil
	}
	p.ShouldTriggerFileCompletion = func(ctx sdk.Context, lines []string, line, col int) (bool, error) {
		if _, _, _, ok := findToken(lines, line, col); ok {
			return true, nil
		}
		if current.ShouldTriggerFileCompletion != nil {
			return current.ShouldTriggerFileCompletion(ctx, lines, line, col)
		}
		return true, nil
	}
	return p, nil
}

func autocompleteItems(choices []pathcomplete.Choice) []sdk.AutocompleteItem {
	items := make([]sdk.AutocompleteItem, len(choices))
	for i, c := range choices {
		items[i] = sdk.AutocompleteItem{Value: c.Value, Label: c.Label}
	}
	return items
}

// fsCompletions completes the text after "/fs ". Values are plain paths: the command takes the rest of the
// line as the path, so spaces need no quotes.
func fsCompletions(cwd, arg string) ([]sdk.AutocompleteItem, error) {
	dir, prefix := pathcomplete.Split(pathcomplete.Decode(arg))
	abs, err := resolveDir(cwd, dir)
	if err != nil {
		return nil, err
	}
	entries, _, err := pathcomplete.List(abs, prefix)
	if err != nil {
		return nil, err
	}
	return autocompleteItems(pathcomplete.Choices(dir, prefix, entries)), nil
}
