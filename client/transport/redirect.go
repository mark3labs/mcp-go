package transport

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ErrCrossOriginRedirect is returned when a server redirects the client to a
// different origin (scheme, host or port). The default HTTP clients refuse such
// redirects so that configured headers and request bodies are never sent to a
// host the caller did not choose.
var ErrCrossOriginRedirect = errors.New("refusing cross-origin redirect")

// ErrTooManyRedirects is returned when a server redirects the client more than
// maxRedirects times.
var ErrTooManyRedirects = errors.New("too many redirects")

// maxRedirects matches the limit of net/http's default redirect policy.
const maxRedirects = 10

// newDefaultHTTPClient returns the HTTP client used by the HTTP-based transports
// when the caller does not supply one. It only follows same-origin redirects.
func newDefaultHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: sameOriginRedirectPolicy}
}

// sameOriginRedirectPolicy is an http.Client CheckRedirect function that
// follows a redirect only when it stays on the origin of the original request.
func sameOriginRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: stopped after %d redirects", ErrTooManyRedirects, maxRedirects)
	}
	if len(via) == 0 {
		return nil
	}
	from := via[0].URL
	if origin(from.Scheme, from.Host) != origin(req.URL.Scheme, req.URL.Host) {
		return fmt.Errorf("%w from %s://%s to %s://%s",
			ErrCrossOriginRedirect, from.Scheme, from.Host, req.URL.Scheme, req.URL.Host)
	}
	return nil
}

// origin returns a normalized scheme://host:port string, filling in the
// default port for http and https.
func origin(scheme, hostport string) string {
	scheme = strings.ToLower(scheme)
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
		port = defaultPort(scheme)
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	return scheme + "://" + net.JoinHostPort(host, port)
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
