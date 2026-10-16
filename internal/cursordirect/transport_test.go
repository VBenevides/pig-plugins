package cursordirect

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunSSEUsesUnframedOrderedAppends(t *testing.T) {
	replies := make(chan []byte, 4)
	var appendCount atomic.Int32
	var wrongPath atomic.Int32
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case appendPath:
			b, err := io.ReadAll(io.LimitReader(r.Body, maxFrame))
			if err != nil {
				t.Error(err)
				return
			}
			appendReq := mustParse(t, b)
			seq := appendReq.num(3)
			if seq != uint64(appendCount.Add(1)-1) {
				t.Error("append sequence changed")
			}
			if !appendReq.has(4) || !appendReq.has(2) {
				t.Error("framed or invalid append")
			}
			replies <- appendReq.data(4)
		case runSSEPath:
			_, b, err := readFrame(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			if mustParse(t, b).text(1) == "" {
				t.Error("missing request id")
			}
			writeFrames(w)
			select {
			case first := <-replies:
				if !mustParse(t, first).has(1) {
					t.Error("first append not Run")
				}
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write(frame(bytesField(2, join(integer(1, 1), bytesField(10, nil)))))
			w.(http.Flusher).Flush()
			select {
			case reply := <-replies:
				if !mustParse(t, reply).has(2) {
					t.Error("missing exec result")
				}
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write(frame(update(1, str(1, "SSE answer"))))
			_, _ = w.Write(frame(update(14, nil)))
			_, _ = w.Write(terminal("{}"))
		default:
			wrongPath.Add(1)
			w.WriteHeader(500)
		}
	})
	c.sse = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, _ := result(t, c, ctx, prompt())
	if m["stopReason"] != "stop" || objects(m["content"])[0]["text"] != "SSE answer" || appendCount.Load() != 2 || wrongPath.Load() != 0 {
		t.Fatalf("%#v appends=%d wrong=%d", m, appendCount.Load(), wrongPath.Load())
	}
}
func TestRunSSEAppendFailureClosesStreamWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	c := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == appendPath {
			w.WriteHeader(403)
			_, _ = w.Write([]byte("synthetic-secret"))
			return
		}
		writeFrames(w)
		<-r.Context().Done()
	})
	c.sse = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m, _ := result(t, c, ctx, prompt())
	if m["stopReason"] != "error" || strings.Contains(text(m["errorMessage"]), "synthetic-secret") {
		t.Fatal(m)
	}
	if calls.Load() > 2 {
		t.Fatal("generation retried")
	}
}
func TestTransportConfiguration(t *testing.T) {
	if _, err := NewWithTransport("automatic"); err == nil {
		t.Fatal("invalid transport accepted")
	}
	c, err := NewWithTransport("sse")
	if err != nil || !c.sse {
		t.Fatal(err)
	}
}
