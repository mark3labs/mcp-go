package transport

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestStderrBuffer_ReadBlocksUntilData verifies Read blocks until Write
// delivers data.
func TestStderrBuffer_ReadBlocksUntilData(t *testing.T) {
	buf := newStderrBuffer()

	result := make(chan string, 1)
	go func() {
		p := make([]byte, 5)
		n, err := buf.Read(p)
		if err != nil {
			result <- "err: " + err.Error()
			return
		}
		result <- string(p[:n])
	}()

	// Give the reader a moment to block, then deliver data.
	time.Sleep(20 * time.Millisecond)
	_, err := buf.Write([]byte("hello"))
	require.NoError(t, err)

	select {
	case got := <-result:
		require.Equal(t, "hello", got)
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not unblock after Write")
	}
}

// TestStderrBuffer_ReadAfterCloseReturnsEOF verifies buffered data stays
// readable after Close and drained reads then return io.EOF.
func TestStderrBuffer_ReadAfterCloseReturnsEOF(t *testing.T) {
	buf := newStderrBuffer()

	_, err := buf.Write([]byte("data"))
	require.NoError(t, err)
	require.NoError(t, buf.Close())

	p := make([]byte, 10)
	n, err := buf.Read(p)
	require.NoError(t, err)
	require.Equal(t, "data", string(p[:n]))

	// Subsequent reads report EOF once the buffer is drained.
	_, err = buf.Read(p)
	require.ErrorIs(t, err, io.EOF)
}

// TestStderrBuffer_WriteAfterCloseFails verifies Write reports
// io.ErrClosedPipe after the stream is closed.
func TestStderrBuffer_WriteAfterCloseFails(t *testing.T) {
	buf := newStderrBuffer()
	require.NoError(t, buf.Close())

	_, err := buf.Write([]byte("late"))
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

// TestStderrBuffer_WriteDropsOldest verifies a full buffer evicts the oldest
// bytes to make room for new output.
func TestStderrBuffer_WriteDropsOldest(t *testing.T) {
	buf := newStderrBuffer()

	// Fill the buffer to capacity with a known pattern.
	first := bytes.Repeat([]byte("a"), stderrBufferSize/2)
	second := bytes.Repeat([]byte("b"), stderrBufferSize/2)
	_, err := buf.Write(first)
	require.NoError(t, err)
	_, err = buf.Write(second)
	require.NoError(t, err)

	// Push the "a" data out: only the latest output must be retained.
	_, err = buf.Write([]byte("tail"))
	require.NoError(t, err)

	require.NoError(t, buf.Close())
	got, err := io.ReadAll(buf)
	require.NoError(t, err)

	// Only the oldest 4 bytes ("a") are dropped to make room for "tail".
	want := append(bytes.Repeat([]byte("a"), stderrBufferSize/2-4),
		bytes.Repeat([]byte("b"), stderrBufferSize/2)...)
	want = append(want, []byte("tail")...)
	require.Len(t, got, stderrBufferSize)
	require.Equal(t, string(want), string(got))
}

// TestStderrBuffer_WriteOversizeChunkKeepsTail verifies a single write larger
// than the buffer retains only its tail.
func TestStderrBuffer_WriteOversizeChunkKeepsTail(t *testing.T) {
	buf := newStderrBuffer()

	// A single write larger than the whole buffer keeps only its tail.
	big := bytes.Repeat([]byte("x"), stderrBufferSize+32)
	_, err := buf.Write(big)
	require.NoError(t, err)

	require.NoError(t, buf.Close())
	got, err := io.ReadAll(buf)
	require.NoError(t, err)
	require.Len(t, got, stderrBufferSize)
}

// TestStdio_StderrFallsBackToRawPipe verifies the non-ring fallback path of
// Stderr() used by NewIO.
func TestStdio_StderrFallsBackToRawPipe(t *testing.T) {
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()
	defer pipeWriter.Close()

	stdio := NewIO(bytes.NewReader(nil), pipeWriter, pipeReader)
	require.Equal(t, pipeReader, stdio.Stderr())
}
