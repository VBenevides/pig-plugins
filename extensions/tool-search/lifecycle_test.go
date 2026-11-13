package toolsearch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/pigtest"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func requestNames(request map[string]any) []string {
	var names []string
	tools, _ := request["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names = append(names, name)
	}
	return names
}

func TestDiscoveryExpiresAfterRequest(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	search, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	todo, err := filepath.Abs("../todo")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(
		pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "todo", "limit": 1})),
		pigtest.Calls(pigtest.Call("todo", map[string]any{"op": "view"})),
		pigtest.Text("done"), pigtest.Text("new request"),
	)
	defer mock.Close()
	result := home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{search, todo}, Prompts: []string{"inspect tasks", "hello again"}})
	for _, event := range result.Events {
		if event["type"] == "extension_error" {
			t.Fatalf("extension error: %v", event)
		}
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	for i, want := range []bool{false, true, true, false} {
		if got := slices.Contains(requestNames(requests[i]), "todo"); got != want {
			t.Fatalf("request %d todo visible=%v want=%v", i, got, want)
		}
	}
}

func TestDiscoveryDoesNotExposeDeniedSchema(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{DisabledTools: map[string]bool{"todo": true}}); err != nil {
		t.Fatal(err)
	}
	search, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	todo, err := filepath.Abs("../todo")
	if err != nil {
		t.Fatal(err)
	}
	mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "todo", "limit": 1})), pigtest.Text("done"))
	defer mock.Close()
	home.Run(t, mock, pigtest.RunOptions{Extensions: []string{search, todo}, Prompt: "find tasks"})
	requests := mock.Requests()
	if len(requests) != 2 || slices.Contains(requestNames(requests[1]), "todo") {
		t.Fatal("denied schema was exposed")
	}
}

func TestSearchBounds(t *testing.T) {
	result, err := boundSearch(sdk.Context{}, map[string]any{"toolName": "tool_search", "input": map[string]any{"limit": float64(21)}})
	if err != nil || result == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestDiscoveryCapsTotalActivations(t *testing.T) {
	pigtest.RequirePig(t)
	home := pigtest.NewHome(t)
	fixture := filepath.Join(t.TempDir(), "many-tools", "extension.go")
	if err := os.MkdirAll(filepath.Dir(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	source := `package fixture
import ("fmt"; sdk "github.com/MichaelKinsy/PiG/extensions/sdk")
func Extension() *sdk.Extension {
 e := sdk.New("many-tools")
 for i:=0; i<30; i++ { e.RegisterTool(sdk.ToolDefinition{Name:fmt.Sprintf("zzgroup_%02d",i), Description:"zzgroup", Exposure:sdk.ToolExposureDeferred, Parameters:map[string]any{"type":"object"}, Execute:func(sdk.Context,map[string]any)(any,error){return nil,nil}}) }
 return e
}`
	if err := os.WriteFile(fixture, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(fixture), "go.mod"), []byte("module fixture\n\ngo 1.24.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	search, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	call := func() pigtest.Reply {
		return pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "zzgroup", "limit": 10}))
	}
	mock := pigtest.NewMockLLM(call(), call(), call(), pigtest.Text("done"))
	defer mock.Close()
	result := home.Run(t, mock, pigtest.RunOptions{Extensions: []string{search, filepath.Dir(fixture)}, Prompt: "find tools"})
	if result.ExitCode != 0 {
		t.Fatalf("session failed: %+v", result)
	}
	requests := mock.Requests()
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	for i, want := range []int{0, 10, 20, 20} {
		count := 0
		for _, name := range requestNames(requests[i]) {
			if strings.HasPrefix(name, "zzgroup_") {
				count++
			}
		}
		if count != want {
			t.Fatalf("request %d schemas=%d want=%d", i, count, want)
		}
	}
}

func TestBackendDiscoveryLoadsWrapper(t *testing.T) {
	pigtest.RequirePig(t)
	for _, tc := range []struct {
		name           string
		denied, direct bool
	}{{"allowed", false, false}, {"denied", true, false}, {"already-active", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			denied := tc.denied
			home := pigtest.NewHome(t)
			if denied {
				if err := toolsettings.Save(filepath.Join(home.AgentDir(), toolsettings.FileName), toolsettings.Config{DisabledTools: map[string]bool{"web_search": true}}); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(t.TempDir(), "search-fixture")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			source := `package fixture
import sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
func Extension() *sdk.Extension {
 e:=sdk.New("search-fixture")
 for _,name:=range []string{"web_search","mcp__parallel__web_search"} { e.RegisterTool(sdk.ToolDefinition{Name:name,Description:name,Exposure:sdk.ToolExposureDeferred,Parameters:map[string]any{"type":"object"},Execute:func(sdk.Context,map[string]any)(any,error){return nil,nil}}) }
 return e
}`
			if tc.direct {
				source = strings.Replace(source, "Exposure:sdk.ToolExposureDeferred", `Exposure:func() sdk.ToolExposure { if name == "web_search" { return sdk.ToolExposureDirect }; return sdk.ToolExposureDeferred }()`, 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "extension.go"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.24.0\n"), 0600); err != nil {
				t.Fatal(err)
			}
			search, err := filepath.Abs(".")
			if err != nil {
				t.Fatal(err)
			}
			mock := pigtest.NewMockLLM(pigtest.Calls(pigtest.Call("tool_search", map[string]any{"query": "mcp__parallel__web_search", "limit": 1})), pigtest.Text("done"), pigtest.Text("next request"))
			defer mock.Close()
			home.RunRPC(t, mock, pigtest.RPCOptions{Extensions: []string{search, dir}, Prompts: []string{"search", "next request"}})
			requests := mock.Requests()
			if len(requests) != 3 {
				t.Fatalf("requests=%d", len(requests))
			}
			names := requestNames(requests[1])
			if slices.Contains(names, "mcp__parallel__web_search") || slices.Contains(names, "web_search") == denied {
				t.Fatalf("declared=%v denied=%v", names, denied)
			}
			if got := slices.Contains(requestNames(requests[2]), "web_search"); got != tc.direct {
				t.Fatalf("next request wrapper active=%v want=%v", got, tc.direct)
			}
		})
	}
}
