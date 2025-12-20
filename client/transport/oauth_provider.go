package transport

// OAuthProvider defines the configuration for a specific OAuth provider.
// This is useful for providers that have known endpoints and behavior that
// might differ from standard discovery mechanisms or require specific settings.
type OAuthProvider struct {
	// AuthorizationEndpoint is the URL for the authorization endpoint
	AuthorizationEndpoint string
	// TokenEndpoint is the URL for the token endpoint
	TokenEndpoint string
	// SupportsDynamicRegistration indicates if the provider supports dynamic client registration
	SupportsDynamicRegistration bool
}

// GitHubProvider is a preset for GitHub's OAuth implementation.
// GitHub does not support dynamic client registration and does not follow
// the standard discovery metadata paths relative to the API domain.
var GitHubProvider = &OAuthProvider{
	AuthorizationEndpoint:       "https://github.com/login/oauth/authorize",
	TokenEndpoint:               "https://github.com/login/oauth/access_token",
	SupportsDynamicRegistration: false,
}
