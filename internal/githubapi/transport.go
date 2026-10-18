package githubapi

import (
	"net/http"
	"strconv"
	"time"
)

const apiVersion = "2026-03-10"

// headerTransport sets the GitHub request headers and returns the response
// unchanged. It does not retry, delay, or limit concurrency.
type headerTransport struct {
	base  http.RoundTripper
	token string
}

func newHeaderTransport(base http.RoundTripper, token string) *headerTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &headerTransport{base: base, token: token}
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Del("X-GIADB-Safe-Request")
	cloned.Header.Set("Accept", "application/vnd.github+json")
	cloned.Header.Set("X-GitHub-Api-Version", apiVersion)
	cloned.Header.Set("User-Agent", "gia-go/0.1")
	if t.token != "" && cloned.Header.Get("Authorization") == "" {
		cloned.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(cloned)
}

func metaFromResponse(resp *http.Response) Meta {
	if resp == nil {
		return Meta{}
	}
	meta := Meta{
		RequestID: resp.Header.Get("X-GitHub-Request-Id"),
		ETag:      resp.Header.Get("ETag"),
	}
	meta.Rate.Limit, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Limit"))
	meta.Rate.Remaining, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	if value, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && value > 0 {
		meta.Rate.ResetAt = time.Unix(value, 0)
	}
	if retryAt, ok := retryAtFromHeader(resp); ok {
		meta.Rate.RetryAt = retryAt
	}
	return meta
}

func retryAtFromHeader(resp *http.Response) (time.Time, bool) {
	value := resp.Header.Get("Retry-After")
	if value == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second), true
	}
	if at, err := http.ParseTime(value); err == nil {
		return at, true
	}
	return time.Time{}, false
}
