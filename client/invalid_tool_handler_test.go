package client

import (
	"context"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestKey marks a context so a test can prove the handler is called with
// the listing request's context.
type requestKey struct{}

// exclusionReport is one call an InvalidToolHandler received.
type exclusionReport struct {
	ctx  context.Context
	tool mcp.Tool
	err  error
}

// exclusionRecorder collects the reports, which arrive on the goroutine that
// listed the tools.
type exclusionRecorder struct {
	mu      sync.Mutex
	reports []exclusionReport
}

func (r *exclusionRecorder) record(ctx context.Context, tool mcp.Tool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, exclusionReport{ctx: ctx, tool: tool, err: err})
}

func (r *exclusionRecorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.reports))
	for _, report := range r.reports {
		names = append(names, report.tool.Name)
	}
	return names
}

func (r *exclusionRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reports)
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// A handler set with WithInvalidToolHandler is told, per excluded tool, which
// definition was dropped and why, instead of only seeing a log line.
func TestInvalidToolHandlerReportsEveryExcludedTool(t *testing.T) {
	recorder := &exclusionRecorder{}
	client, version := connectHeaderClient(t,
		&headerToolsServer{t: t, listings: []string{annotatedTools}},
		nil, WithInvalidToolHandler(recorder.record))
	require.Equal(t, mcp.ProtocolVersion20260728, version)

	ctx := context.WithValue(t.Context(), requestKey{}, "request-1")
	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)

	// Filtering is unchanged: the invalid tools stay excluded.
	assert.Equal(t, []string{"query", "plain"}, toolNames(result.Tools))

	// Every excluded tool is reported, with its own validation error and the
	// context of the request that listed it.
	assert.Equal(t, []string{"empty_header", "number_header", "duplicate_header"}, recorder.names())
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, report := range recorder.reports {
		assert.Equal(t, "request-1", report.ctx.Value(requestKey{}))
		assert.NotNil(t, report.tool.InputSchema.Properties)
	}
	assert.ErrorContains(t, recorder.reports[0].err, "x-mcp-header must be a non-empty string")
	assert.ErrorContains(t, recorder.reports[1].err, "can only be applied to primitive types")
	assert.ErrorContains(t, recorder.reports[2].err, "duplicate x-mcp-header")
	// The reported definition keeps its annotations, so a caller can show which
	// annotation the server must fix.
	assert.Contains(t, recorder.reports[0].tool.InputSchema.Properties, "region")
}

// Each page of a listing reports its own exclusions.
func TestInvalidToolHandlerReportsEveryPage(t *testing.T) {
	recorder := &exclusionRecorder{}
	client, _ := connectHeaderClient(t,
		&headerToolsServer{t: t, listings: []string{annotatedTools}},
		nil, WithInvalidToolHandler(recorder.record))

	for range 2 {
		result, err := client.ListToolsByPage(t.Context(), mcp.ListToolsRequest{})
		require.NoError(t, err)
		assert.Equal(t, []string{"query", "plain"}, toolNames(result.Tools))
	}

	excluded := []string{"empty_header", "number_header", "duplicate_header"}
	assert.Equal(t, append(append([]string{}, excluded...), excluded...), recorder.names())
}

// A legacy connection lists the annotated tools as before, so nothing is
// excluded and nothing is reported.
func TestInvalidToolHandlerNotCalledForLegacyConnections(t *testing.T) {
	recorder := &exclusionRecorder{}
	client, version := connectHeaderClient(t,
		&headerToolsServer{t: t, legacy: true, listings: []string{annotatedTools}},
		nil, WithInvalidToolHandler(recorder.record))
	require.Equal(t, mcp.LATEST_LEGACY_PROTOCOL_VERSION, version)

	assert.Len(t, listToolNames(t, client), 5)
	assert.Equal(t, 0, recorder.len())
}

// Without the option, exclusion keeps working exactly as before and the nil
// handler is never dereferenced.
func TestListToolsExcludesInvalidHeaderAnnotationsWithoutHandler(t *testing.T) {
	client, _ := connectHeaderClient(t,
		&headerToolsServer{t: t, listings: []string{annotatedTools}}, nil)

	assert.Equal(t, []string{"query", "plain"}, listToolNames(t, client))
}
