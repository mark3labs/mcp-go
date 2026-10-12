package transport

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Authorization header always carries a scheme. A token stored without a
// type used to produce " <token>", which goes out as the bare token.
func TestOAuthHandler_GetAuthorizationHeader_TokenType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tokenType string
		want      string
	}{
		{name: "Bearer", tokenType: "Bearer", want: "Bearer abc"},
		{name: "lower case", tokenType: "bearer", want: "Bearer abc"},
		{name: "upper case", tokenType: "BEARER", want: "Bearer abc"},
		{name: "no type", tokenType: "", want: "Bearer abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryTokenStore()
			require.NoError(t, store.SaveToken(t.Context(), &Token{
				AccessToken: "abc",
				TokenType:   tc.tokenType,
				ExpiresAt:   time.Now().Add(time.Hour),
			}))
			handler := NewOAuthHandler(OAuthConfig{ClientID: "client", TokenStore: store})

			header, err := handler.GetAuthorizationHeader(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.want, header)
		})
	}
}
