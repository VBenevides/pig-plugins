package cursordirect

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func TestFragmentedAnswerDoesNotSpendSnapshotLifetimeBudget(t *testing.T) {
	for _, pieces := range []int{1, 2000} {
		t.Run(strconv.Itoa(pieces), func(t *testing.T) {
			var frames [][]byte
			for i := 0; i < pieces; i++ {
				frames = append(frames, frame(update(1, str(1, strings.Repeat("x", 128000/pieces)))))
			}
			frames = append(frames, frame(update(14, nil)), terminal(`{}`))
			client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { writeFrames(w, join(frames...)) })
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			stream, err := client.Stream(model(), prompt(), sdk.ProviderStreamOptions{Signal: ctx, Values: map[string]any{"apiKey": "synthetic"}})
			if err != nil {
				t.Fatal(err)
			}
			var first map[string]any
			for event := range stream.Events(ctx) {
				if first == nil && event["type"] == "text_delta" {
					first = event["partial"].(map[string]any)
				}
			}
			message := stream.Result()
			if first == nil || len(text(array(first["content"])[0].(map[string]any)["text"])) != 128000/pieces {
				t.Fatal("earlier snapshot mutated")
			}
			if message["stopReason"] == "error" {
				t.Fatalf("fragmented answer failed: %v", message["errorMessage"])
			}
			content := array(message["content"])
			if len(content) != 1 || len(text(content[0].(map[string]any)["text"])) != 128000 {
				t.Fatalf("answer lost: blocks=%d", len(content))
			}
		})
	}
}

func TestCursorBackpressureCancellationWithoutConsumer(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeFrames(w, frame(update(1, str(1, "one"))), frame(update(1, str(1, "two"))), frame(update(14, nil)), terminal(`{}`))
	})
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.Stream(model(), prompt(), sdk.ProviderStreamOptions{Signal: ctx, Values: map[string]any{"apiKey": "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	done := make(chan map[string]any, 1)
	go func() { done <- stream.Result() }()
	select {
	case message := <-done:
		if message["stopReason"] != "aborted" {
			t.Fatalf("terminal=%v", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked producer did not cancel")
	}
}

func TestCursorLifetimeFrameWorkBudget(t *testing.T) {
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/connect+proto")
		for range 65537 {
			if _, err := w.Write(frame(bytesField(3, nil))); err != nil {
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	message, _ := result(t, client, ctx, prompt())
	if !strings.Contains(text(message["errorMessage"]), "frame work budget exceeded") {
		t.Fatalf("unexpected terminal: %v", message)
	}
}

func TestCursorInterleavedFragmentedTextAndThinking(t *testing.T) {
	var frames [][]byte
	for group := 0; group < 8; group++ {
		kind := 1
		if group%2 == 0 {
			kind = 4
		}
		for range 250 {
			frames = append(frames, frame(update(kind, str(1, strings.Repeat("x", 64)))))
		}
	}
	frames = append(frames, frame(update(14, nil)), terminal(`{}`))
	client := fakeClient(t, func(w http.ResponseWriter, _ *http.Request) { writeFrames(w, join(frames...)) })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	stream, err := client.Stream(model(), prompt(), sdk.ProviderStreamOptions{Signal: ctx, Values: map[string]any{"apiKey": "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	for range stream.Events(ctx) {
	}
	message := stream.Result()
	parts := objects(message["content"])
	if message["stopReason"] != "stop" || len(parts) != 8 {
		t.Fatalf("terminal=%v blocks=%d", message["errorMessage"], len(parts))
	}
	for index, part := range parts {
		key := "text"
		if index%2 == 0 {
			key = "thinking"
		}
		if len(text(part[key])) != 16000 {
			t.Fatalf("block %d lost output", index)
		}
	}
}
