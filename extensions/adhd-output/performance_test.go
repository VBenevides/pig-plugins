package adhdoutput

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/VBenevides/pig-plugins/internal/pigtest"
)

// TestPerformanceComparison records actual startup, idle process-tree memory,
// and handler latency. It uses no timing thresholds and requests no model turns.
func TestPerformanceComparison(t *testing.T) {
	pigtest.RequirePig(t)
	for _, variant := range []string{"absent", "disabled", "enabled"} {
		t.Run(variant, func(t *testing.T) {
			home := pigtest.NewHome(t)
			mock := pigtest.NewMockLLM()
			defer mock.Close()
			home.WriteModels(t, map[string]pigtest.ProviderModels{"mock": {Mock: mock, Models: []string{"mock-model"}}})
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			args := []string{"--mode", "rpc", "--no-session", "--provider", "mock", "--model", "mock-model"}
			if variant != "absent" {
				args = append(args, "-e", extensionPath(t))
			}
			if variant == "enabled" {
				args = append(args, "--adhd", "true")
			}
			cmd := exec.CommandContext(ctx, "pig", args...)
			cmd.Dir = home.Work
			cmd.Env = home.Env(nil)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			start := time.Now()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			reader := bufio.NewReaderSize(stdout, 1<<20)
			roundtrip := func(command map[string]any, prompt bool) time.Duration {
				t.Helper()
				start := time.Now()
				data, err := json.Marshal(command)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := stdin.Write(append(data, '\n')); err != nil {
					t.Fatal(err)
				}
				for {
					line, err := reader.ReadBytes('\n')
					if err != nil {
						t.Fatalf("RPC ended: %v", err)
					}
					var event map[string]any
					if err := json.Unmarshal(line, &event); err != nil {
						t.Fatal(err)
					}
					if event["type"] == "extension_error" || event["notifyType"] == "error" {
						t.Fatal(event)
					}
					if event["type"] != "response" {
						continue
					}
					if event["success"] == false {
						t.Fatal(event)
					}
					if !prompt && event["command"] == command["type"] {
						return time.Since(start)
					}
					data, _ := event["data"].(map[string]any)
					if prompt && event["command"] == "prompt" && data["disposition"] == "handled" {
						return time.Since(start)
					}
				}
			}
			roundtrip(map[string]any{"type": "get_state"}, false)
			startup := time.Since(start)
			if variant != "absent" {
				roundtrip(map[string]any{"type": "prompt", "message": "/adhd status"}, true)
			}
			rss, pss, memoryErr := processTreeMemory(cmd.Process.Pid)
			latencies := make([]time.Duration, 20)
			for i := range latencies {
				if variant == "absent" {
					latencies[i] = roundtrip(map[string]any{"type": "get_state"}, false)
				} else {
					latencies[i] = roundtrip(map[string]any{"type": "prompt", "message": "/adhd status"}, true)
				}
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			if len(mock.Requests()) != 0 {
				t.Fatal("startup/status requests triggered model calls")
			}
			if memoryErr != nil {
				t.Logf("idle RSS/PSS unavailable: %v", memoryErr)
			}
			t.Logf("%s: startup=%s idle process-tree RSS=%d KiB PSS=%d KiB handler p50=%s p95=%s (%s)", variant, startup, rss, pss, latencies[9], latencies[18], map[bool]string{true: "get_state baseline", false: "/adhd status"}[variant == "absent"])
		})
	}
}

func processTreeMemory(root int) (rss, pss int64, err error) {
	pending := []int{root}
	for count := 0; len(pending) > 0; count++ {
		if count >= 128 {
			return rss, pss, fmt.Errorf("process tree exceeds 128 processes")
		}
		pid := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		base := filepath.Join("/proc", strconv.Itoa(pid))
		data, readErr := os.ReadFile(filepath.Join(base, "smaps_rollup"))
		if readErr != nil {
			return rss, pss, readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				continue
			}
			if fields[0] != "Rss:" && fields[0] != "Pss:" {
				continue
			}
			value, parseErr := strconv.ParseInt(fields[1], 10, 64)
			if parseErr != nil {
				return rss, pss, parseErr
			}
			if fields[0] == "Rss:" {
				rss += value
			} else {
				pss += value
			}
		}
		children, readErr := os.ReadFile(filepath.Join(base, "task", strconv.Itoa(pid), "children"))
		if readErr != nil {
			return rss, pss, readErr
		}
		for _, child := range strings.Fields(string(children)) {
			id, parseErr := strconv.Atoi(child)
			if parseErr != nil {
				return rss, pss, parseErr
			}
			pending = append(pending, id)
		}
	}
	return rss, pss, nil
}
