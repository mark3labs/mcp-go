package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPServer_SendProgressNotificationToClient(t *testing.T) {
	total := 10.0
	message := "copying"
	tests := []struct {
		name         string
		notification mcp.ProgressNotification
		want         map[string]any
	}{
		{
			name:         "token and progress",
			notification: mcp.NewProgressNotification("tok", 3, nil, nil),
			want:         map[string]any{"progressToken": "tok", "progress": 3.0},
		},
		{
			name:         "total and message",
			notification: mcp.NewProgressNotification(7, 3, &total, &message),
			want:         map[string]any{"progressToken": 7, "progress": 3.0, "total": 10.0, "message": "copying"},
		},
		{
			name:         "no token sends nothing",
			notification: mcp.NewProgressNotification(nil, 3, nil, nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMCPServer("test-server", "1.0.0")
			ch := make(chan mcp.JSONRPCNotification, 1)
			session := &sessionTestClient{sessionID: "session-1", notificationChannel: ch}
			session.Initialize()
			require.NoError(t, s.RegisterSession(t.Context(), session))

			require.NoError(t, s.SendProgressNotificationToClient(s.WithContext(t.Context(), session), tt.notification))

			select {
			case got := <-ch:
				require.NotNil(t, tt.want, "unexpected notification %+v", got)
				assert.Equal(t, string(mcp.MethodNotificationProgress), got.Method)
				assert.Equal(t, tt.want, got.Params.AdditionalFields)
			default:
				assert.Nil(t, tt.want, "no notification sent")
			}
		})
	}
}

func TestMCPServer_SendProgressNotificationToClientWithoutSession(t *testing.T) {
	s := NewMCPServer("test-server", "1.0.0")
	err := s.SendProgressNotificationToClient(t.Context(), mcp.NewProgressNotification("tok", 1, nil, nil))
	assert.ErrorIs(t, err, ErrNotificationNotInitialized)
}
