package stream

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func collect(t *testing.T, raw string) []Event {
	t.Helper()
	var got []Event
	err := Read(context.Background(), strings.NewReader(raw), func(e Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func TestReadsNamedEvents(t *testing.T) {
	got := collect(t, "event: step\ndata: {\"step\":1}\n\nevent: done\ndata: {}\n\n")
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Name != "step" || got[0].Data != `{"step":1}` {
		t.Errorf("first event = %+v", got[0])
	}
	if got[1].Name != "done" {
		t.Errorf("second event = %+v", got[1])
	}
}

// Multi-line data is joined with newlines, per the SSE spec — a JSON payload split across
// lines would otherwise decode as garbage.
func TestJoinsMultiLineData(t *testing.T) {
	got := collect(t, "event: step\ndata: line one\ndata: line two\n\n")
	if len(got) != 1 || got[0].Data != "line one\nline two" {
		t.Fatalf("multi-line data not joined: %+v", got)
	}
}

func TestIgnoresCommentsAndKeepAlives(t *testing.T) {
	got := collect(t, ": keep-alive\n\nevent: step\ndata: x\n\n: ping\n\n")
	if len(got) != 1 || got[0].Name != "step" {
		t.Fatalf("comments leaked into the event stream: %+v", got)
	}
}

// A stream that ends without its terminating blank line still has one frame to deliver;
// dropping it would lose the final answer.
func TestDeliversATrailingFrameWithoutABlankLine(t *testing.T) {
	got := collect(t, "event: done\ndata: {}")
	if len(got) != 1 || got[0].Name != "done" {
		t.Fatalf("trailing frame lost: %+v", got)
	}
}

func TestStripsOnlyOneLeadingSpace(t *testing.T) {
	got := collect(t, "event: step\ndata:  двойной\n\n")
	if got[0].Data != " двойной" {
		t.Errorf("data = %q — exactly one leading space is stripped", got[0].Data)
	}
}

func TestHandlerErrorStopsTheStream(t *testing.T) {
	stop := errors.New("enough")
	var seen int
	err := Read(context.Background(), strings.NewReader("event: a\ndata: 1\n\nevent: b\ndata: 2\n\n"),
		func(Event) error {
			seen++
			return stop
		})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the handler's error", err)
	}
	if seen != 1 {
		t.Errorf("handler called %d times after returning an error", seen)
	}
}

func TestCancelledContextStopsTheStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Read(ctx, strings.NewReader("event: a\ndata: 1\n\n"), func(Event) error {
		t.Error("handler ran with a cancelled context")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// A tool result can be far larger than bufio's 64KB default; truncating mid-payload would
// surface as a JSON decode error that looks like a server bug.
func TestHandlesFramesLargerThanTheDefaultBuffer(t *testing.T) {
	big := strings.Repeat("x", 200_000)
	got := collect(t, "event: step\ndata: "+big+"\n\n")
	if len(got) != 1 || len(got[0].Data) != len(big) {
		t.Fatalf("large frame truncated: got %d bytes", len(got[0].Data))
	}
}
