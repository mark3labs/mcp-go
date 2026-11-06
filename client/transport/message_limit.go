package transport

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// DefaultMaxMessageBytes is the largest message the SSE and Streamable HTTP
// client transports read from a server by default, 32 MiB: a JSON response
// body, or the data of one SSE event.
const DefaultMaxMessageBytes int64 = 32 << 20

// ErrMessageTooLarge is the error of a message from the server over the
// bound. See WithMaxMessageBytes and WithHTTPMaxMessageBytes.
var ErrMessageTooLarge = errors.New("message from the server exceeds the size bound")

// maxErrorBodyBytes bounds how much of an error response is read for the
// error it is reported in.
const maxErrorBodyBytes = 64 << 10

// readErrorBody reads at most maxErrorBodyBytes of an error response's body.
func readErrorBody(r io.Reader) []byte {
	body, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	return body
}

// boundedReader is a reader that fails with ErrMessageTooLarge once its
// source has more than n bytes left.
type boundedReader struct {
	r io.Reader
	n int64
}

// newBoundedReader returns r bounded at limit bytes; zero or a negative limit
// leaves it unbounded.
func newBoundedReader(r io.Reader, limit int64) io.Reader {
	if limit <= 0 {
		return r
	}
	return &boundedReader{r: r, n: limit}
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.n <= 0 {
		// One byte past the bound tells a body that fills it from a longer one.
		var probe [1]byte
		n, err := b.r.Read(probe[:])
		if n > 0 {
			return 0, ErrMessageTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.n {
		p = p[:b.n]
	}
	n, err := b.r.Read(p)
	b.n -= int64(n)
	return n, err
}

type sseLineReader struct {
	br     *bufio.Reader
	skipLF bool
}

// readLine reads one line of an SSE stream, including its CR, LF, or CRLF
// ending. A line may carry limit bytes of data besides
// its field name and line ending; a longer one is ErrMessageTooLarge. Zero or
// a negative limit leaves lines unbounded.
func (r *sseLineReader) readLine(limit int64) (string, error) {
	if limit > 0 {
		limit += int64(len("event: \r\n"))
	}
	var line []byte
	for {
		if _, err := r.br.Peek(1); err != nil {
			return string(line), err
		}
		chunk, _ := r.br.Peek(r.br.Buffered())
		if r.skipLF {
			r.skipLF = false
			if chunk[0] == '\n' {
				_, _ = r.br.Discard(1)
				continue
			}
		}
		n := bytes.IndexAny(chunk, "\r\n")
		complete := n >= 0
		if complete {
			n++
			if chunk[n-1] == '\r' {
				r.skipLF = true
				if n < len(chunk) && chunk[n] == '\n' {
					n++
					r.skipLF = false
				}
			}
		} else {
			n = len(chunk)
		}
		lineBytes := int64(len(line) + n)
		if r.skipLF {
			// Reserve the optional LF even when it arrives in the next read.
			lineBytes++
		}
		if limit > 0 && lineBytes > limit {
			return "", ErrMessageTooLarge
		}
		line = append(line, chunk[:n]...)
		_, _ = r.br.Discard(n)
		if complete {
			return string(line), nil
		}
	}
}
