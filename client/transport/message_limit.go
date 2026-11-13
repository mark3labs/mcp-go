package transport

import (
	"bufio"
	"errors"
	"io"
	"strings"
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
	reader *bufio.Reader
	skipLF bool
}

func newSSELineReader(reader io.Reader) *sseLineReader {
	return &sseLineReader{reader: bufio.NewReader(reader)}
}

// readLine accepts LF, CRLF, and CR line endings, without returning them.
// A CR completes the line immediately; an optional LF is consumed on the next
// call, so a server holding the stream open cannot delay event dispatch.
// A line may carry limit bytes besides the longest field prefix ("event: ");
// line endings do not count. A longer line is ErrMessageTooLarge. Zero or a
// negative limit leaves lines unbounded.
// An unterminated final line is returned with io.EOF, as with ReadString.
func (r *sseLineReader) readLine(limit int64) (string, error) {
	var line strings.Builder
	for {
		b, err := r.reader.ReadByte()
		if err != nil {
			return line.String(), err
		}
		if r.skipLF {
			r.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			r.skipLF = true
			return line.String(), nil
		case '\n':
			return line.String(), nil
		}
		if limit > 0 && int64(line.Len()) >= limit && int64(line.Len())-limit >= int64(len("event: ")) {
			return "", ErrMessageTooLarge
		}
		line.WriteByte(b)
	}
}
