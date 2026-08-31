// Package stream reads Server-Sent Events.
//
// Hand-rolled rather than pulled in: the wire format is a handful of lines, and the one
// behaviour that matters here — surfacing each frame the moment it arrives rather than at
// the end — is easier to guarantee in twenty lines than to verify in a dependency.
package stream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Event is one decoded frame. Name is the SSE event type; Data is the raw payload, which
// callers decode according to that type.
type Event struct {
	Name string
	Data string
}

// Read consumes an SSE response, calling fn for every complete frame.
//
// It returns when the stream ends, when fn returns an error, or when ctx is cancelled.
// A frame is delivered as soon as its terminating blank line arrives — buffering until EOF
// would defeat the point of streaming an investigation someone is watching.
func Read(ctx context.Context, body io.Reader, fn func(Event) error) error {
	scanner := bufio.NewScanner(body)
	// Tool results can be long; the 64KB default would truncate a frame mid-payload and
	// produce a JSON decode error that looks like a server bug.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var name string
	var data strings.Builder

	dispatch := func() error {
		if name == "" && data.Len() == 0 {
			return nil
		}
		ev := Event{Name: name, Data: data.String()}
		name = ""
		data.Reset()
		return fn(ev)
	}

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()

		switch {
		case line == "":
			// Blank line terminates a frame.
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// Comment / keep-alive.
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}

	if err := scanner.Err(); err != nil {
		// A cancelled context surfaces here as a read error; report the cause, not the symptom.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("read stream: %w", err)
	}
	// A stream that ends without a trailing blank line still has one frame to deliver.
	return dispatch()
}

// ErrStopped is returned by a handler that wants Read to stop without it being an error.
var ErrStopped = errors.New("stream stopped by handler")

// Open issues the request and streams the response. The caller closes nothing; Open owns
// the body for the lifetime of the callback.
func Open(ctx context.Context, client *http.Client, req *http.Request, fn func(Event) error) error {
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stream %s: HTTP %d", req.URL.Path, resp.StatusCode)
	}

	err = Read(ctx, resp.Body, fn)
	if errors.Is(err, ErrStopped) {
		return nil
	}
	return err
}
