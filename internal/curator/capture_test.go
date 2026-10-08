package curator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func ev(id string) EventIn {
	return EventIn{ID: id, SessionID: "s", Category: CategoryUser, Content: new(id)}
}
func acknowledge(outcome func(string) string, seen *[]IngestRequest) Transport {
	return func(_ context.Context, r IngestRequest) (IngestResponse, error) {
		if seen != nil {
			*seen = append(*seen, r)
		}
		out := IngestResponse{}
		for _, e := range r.Events {
			out.Results = append(out.Results, ItemResult{ID: e.ID, Outcome: outcome(e.ID)})
		}
		for _, g := range r.Gaps {
			out.Gaps = append(out.Gaps, ItemResult{ID: "gap_" + g.EventID, Outcome: OutcomeDurable})
		}
		return out, nil
	}
}
func TestCaptureBatchesAndOverflow(t *testing.T) {
	var seen []IngestRequest
	c := NewCapture(acknowledge(func(string) string { return OutcomeDurable }, &seen), CaptureOptions{MaxQueue: 2, BatchSize: 1})
	c.Enqueue([]EventIn{ev("a"), ev("b"), ev("c"), ev("d")})
	if got := c.Stats(); got.Queued != 2 || got.Gaps != 2 {
		t.Fatal(got)
	}
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.Stats(); got.Durable != 2 || got.Queued != 0 || got.Gaps != 0 {
		t.Fatal(got)
	}
	var ids []string
	for _, r := range seen {
		for _, g := range r.Gaps {
			ids = append(ids, g.EventID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"c", "d"}) {
		t.Fatal(ids)
	}
}
func TestCaptureIndependentOutcomes(t *testing.T) {
	c := NewCapture(acknowledge(func(id string) string {
		switch id {
		case "bad":
			return OutcomeRejected
		case "slow":
			return OutcomeFailed
		default:
			return OutcomeDurable
		}
	}, nil), CaptureOptions{MaxAttempts: 5})
	c.Enqueue([]EventIn{ev("a"), ev("bad"), ev("slow"), ev("b")})
	c.Flush(context.Background())
	if got := c.Stats(); got.Durable != 2 || got.Rejected != 1 || got.Queued != 1 {
		t.Fatal(got)
	}
}
func TestCaptureMemoryInspectionRejectionIsNotAProblem(t *testing.T) {
	var problems []string
	c := NewCapture(func(_ context.Context, r IngestRequest) (IngestResponse, error) {
		out := IngestResponse{}
		for _, e := range r.Events {
			code := "other"
			if e.ID == "inspect" {
				code = codeMemoryInspection
			}
			out.Results = append(out.Results, ItemResult{ID: e.ID, Outcome: OutcomeRejected, Code: code})
		}
		return out, nil
	}, CaptureOptions{BatchSize: 1, OnProblem: func(m string) { problems = append(problems, m) }})
	c.Enqueue([]EventIn{ev("inspect"), ev("bad")})
	c.Flush(context.Background())
	if got := c.Stats(); got.Rejected != 2 || got.Queued != 0 {
		t.Fatal(got)
	}
	if len(problems) != 1 {
		t.Fatal(problems)
	}
}
func TestCaptureRetriesBecomeGapsAndRecover(t *testing.T) {
	calls := 0
	up := false
	var seen []IngestRequest
	ack := acknowledge(func(string) string { return OutcomeDuplicate }, &seen)
	c := NewCapture(func(ctx context.Context, r IngestRequest) (IngestResponse, error) {
		calls++
		if !up {
			return IngestResponse{}, errors.New("down")
		}
		return ack(ctx, r)
	}, CaptureOptions{MaxAttempts: 2})
	c.Enqueue([]EventIn{ev("a")})
	c.Flush(context.Background())
	if calls != 1 || c.Stats().Queued != 1 {
		t.Fatal(calls, c.Stats())
	}
	c.Flush(context.Background())
	if !reflect.DeepEqual(c.PendingGaps(), []Gap{{SessionID: "s", EventID: "a"}}) {
		t.Fatal(c.PendingGaps())
	}
	up = true
	c.Flush(context.Background())
	if c.Stats().Gaps != 0 {
		t.Fatal(c.Stats())
	}
}
func TestCaptureDeadlineThenAbandon(t *testing.T) {
	c := NewCapture(func(ctx context.Context, _ IngestRequest) (IngestResponse, error) {
		<-ctx.Done()
		return IngestResponse{}, ctx.Err()
	}, CaptureOptions{})
	c.Enqueue([]EventIn{ev("a")})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if c.Stats().Queued != 1 {
		t.Fatal(c.Stats())
	}
	c.AbandonQueue()
	if got := c.PendingGaps(); !reflect.DeepEqual(got, []Gap{{SessionID: "s", EventID: "a"}}) {
		t.Fatal(got)
	}
}
func TestCaptureMismatchedResponseDoesNotAcknowledge(t *testing.T) {
	c := NewCapture(func(context.Context, IngestRequest) (IngestResponse, error) {
		return IngestResponse{Results: []ItemResult{{ID: "wrong", Outcome: OutcomeDurable}}}, nil
	}, CaptureOptions{MaxAttempts: 2})
	c.Enqueue([]EventIn{ev("a")})
	c.Flush(context.Background())
	if got := c.Stats(); got.Durable != 0 || got.Queued != 1 {
		t.Fatal(got)
	}
	c.Flush(context.Background())
	if got := c.Stats(); got.Durable != 0 || got.Gaps != 1 {
		t.Fatal(got)
	}
}
