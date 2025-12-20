package transport

// OAuthProvider defines the configuration for a specific OAuth provider.
// This is used to bypass auto-discovery for providers that do not support RFC 8414
// or have split domains (e.g. GitHub).
type OAuthProvider struct {
	AuthorizationEndpoint       string
	TokenEndpoint               string
	SupportsDynamicRegistration bool
}

// GitHubProvider is the preset configuration for GitHub OAuth.
// GitHub does not support RFC 7591 dynamic registration and uses split domains for API and generic OAuth.
var GitHubProvider = &OAuthProvider{
	AuthorizationEndpoint:       "https://github.com/login/oauth/authorize",
	TokenEndpoint:               "https://github.com/login/oauth/access_token",
	SupportsDynamicRegistration: false,
}
