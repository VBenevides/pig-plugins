package askuserquestion

import (
	"encoding/json"
	"github.com/VBenevides/pig-plugins/internal/ask"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"path/filepath"
	"strings"
	"testing"
)

func rawParams(p ask.Params) map[string]any {
	data, _ := json.Marshal(p)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	return raw
}
func question(text string, multi bool) ask.Question {
	return ask.Question{Question: text, Header: "Choice", Multi: multi, Options: []ask.Option{{Label: "Go", Description: "Compiled", Preview: "package main"}, {Label: "Rust", Description: "Compiled"}}}
}
func TestPiGRPCAnswersAndCancellation(t *testing.T) {
	pigtest.RequirePig(t)
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("answers", func(t *testing.T) {
		home := pigtest.NewHome(t)
		params := ask.Params{Questions: []ask.Question{question("Single?", false), question("Multiple?", true), question("Custom?", false)}}
		mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("ask_user_question", rawParams(params))), pigtest.Text("done"))
		defer mock.Close()
		selects, inputs := 0, 0
		result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"Ask me"}, Dialog: func(request map[string]any) map[string]any {
			switch request["method"] {
			case "select":
				selects++
				options := request["options"].([]any)
				index := 0
				if selects == 2 {
					index = len(options) - 1
				}
				return map[string]any{"value": options[index]}
			case "input":
				inputs++
				value := "2,1,2"
				if inputs == 2 {
					value = "Zig\nnext"
				}
				return map[string]any{"value": value}
			}
			return map[string]any{"cancelled": true}
		}})
		results := pigtest.ToolResults(mock)
		if len(results) != 1 || !strings.Contains(results[0], `"Single?"="Go". selected preview: package main.`) || !strings.Contains(results[0], `"Multiple?"="Rust, Go".`) || !strings.Contains(results[0], "\"Custom?\"=\"Zig\nnext\".") {
			t.Fatalf("answers %v; dialogs=%v stderr=%s", results, result.Asked, result.Stderr)
		}
		if selects != 2 || inputs != 2 {
			t.Fatalf("dialogs: %#v", result.Asked)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		home := pigtest.NewHome(t)
		mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("ask_user_question", rawParams(ask.Params{Questions: []ask.Question{question("Pick?", false)}}))), pigtest.Text("done"))
		defer mock.Close()
		result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{ext}, Prompts: []string{"Ask me"}})
		results := pigtest.ToolResults(mock)
		if len(result.Asked) != 1 || len(results) != 1 || results[0] != "User declined to answer questions" {
			t.Fatalf("cancel %v; dialogs=%v stderr=%s", results, result.Asked, result.Stderr)
		}
	})
}
func TestPiGPrintRejectsInteractiveTool(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("ask_user_question", rawParams(ask.Params{Questions: []ask.Question{question("Pick?", false)}}))), pigtest.Text("done"))
	defer mock.Close()
	home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
	ext, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	result := home.RunPig(t, pigtest.RunOptions{Extensions: []string{ext}}, "-p", "--provider", "mock", "--model", "mock-model")
	if result.ExitCode != 0 {
		t.Fatalf("pig: %s %s", result.Stdout, result.Stderr)
	}
	results := pigtest.ToolResults(mock)
	if len(results) != 1 || !strings.Contains(results[0], "UI not available (running in non-interactive mode)") {
		t.Fatalf("no-UI result: %v; stderr=%s", results, result.Stderr)
	}
}
