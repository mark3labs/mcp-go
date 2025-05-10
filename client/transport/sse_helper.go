package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

type sseEvent struct {
	event string
	data  string
}

// ReadSSEStream continuously reads the SSE stream and processes events.
func ReadSSEStream(ctx context.Context, reader io.ReadCloser, onEvent func(event sseEvent)) error {
	defer func(reader io.ReadCloser) {
		err := reader.Close()
		if err != nil {
			fmt.Printf("Error closing reader: %v\n", err)
		}
	}(reader)

	br := bufio.NewReader(reader)
	var event, data strings.Builder

	processEvent := func() {
		if event.Len() > 0 || data.Len() > 0 {
			onEvent(sseEvent{event: event.String(), data: data.String()})
			event.Reset()
			data.Reset()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			line, err := br.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// Handle last event when EOF
					processEvent()
					return nil
				}
				return fmt.Errorf("error reading SSE stream: %w", err)
			}

			// Remove only newline markers
			line = strings.TrimRight(line, "\r\n")

			switch {
			case strings.HasPrefix(line, "event:"):
				event.Reset()
				event.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "event:")))
			case strings.HasPrefix(line, "data:"):
				if data.Len() > 0 {
					data.WriteString("\n")
				}
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case line == "":
				processEvent()
			}
		}
	}
}
