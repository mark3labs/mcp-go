package transport

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// maxRedirects matches the limit net/http applies when CheckRedirect is nil.
const maxRedirects = 10

// ErrCrossOriginRedirect is returned when a server redirects a request to a
// different scheme, host or port. The transports' default HTTP clients refuse
// such redirects, because the redirected request would carry the configured
// headers, the OAuth bearer token and, for 307 and 308, the request body to a
// destination the server chose.
var ErrCrossOriginRedirect = errors.New("refusing cross-origin redirect")

// sameOriginRedirectPolicy is the CheckRedirect policy of the HTTP clients the
// transports create when the caller does not supply one. It follows a
// redirect only when the new URL has the scheme, host and port of the request
// that started the chain, and at most maxRedirects times.
func sameOriginRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	origin := via[0].URL
	if !strings.EqualFold(req.URL.Scheme, origin.Scheme) ||
		!strings.EqualFold(req.URL.Hostname(), origin.Hostname()) ||
		effectivePort(req.URL.Scheme, req.URL.Port()) != effectivePort(origin.Scheme, origin.Port()) {
		return fmt.Errorf("%w: %s://%s to %s://%s", ErrCrossOriginRedirect,
			origin.Scheme, origin.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}

func effectivePort(scheme, port string) string {
	if port != "" {
		return port
	}
	switch strings.ToLower(scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// newDefaultHTTPClient returns the HTTP client a transport uses when the
// caller does not supply one.
func newDefaultHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: sameOriginRedirectPolicy}
}
