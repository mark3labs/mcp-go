package server

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestPaginationLargePositiveLimitAfterCursor(t *testing.T) {
	s := NewMCPServer("test", "1", WithPaginationLimit(int(^uint(0)>>1)))
	items := []mcp.Prompt{{Name: "a"}, {Name: "b"}}
	cursor := mcp.Cursor(base64.StdEncoding.EncodeToString([]byte("a")))
	got, next, err := listByPagination(t.Context(), s, cursor, items)
	require.NoError(t, err)
	require.Equal(t, []mcp.Prompt{{Name: "b"}}, got)
	require.Empty(t, next)
}
