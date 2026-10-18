package githubapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRoundTripSetsHeadersOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	transport := newHeaderTransport(
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			if request.Header.Get("X-GitHub-Api-Version") != apiVersion {
				t.Errorf("API version = %q", request.Header.Get("X-GitHub-Api-Version"))
			}
			if request.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("Accept = %q", request.Header.Get("Accept"))
			}
			if request.Header.Get("Authorization") != "Bearer secret" {
				t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("limited")),
				Request:    request,
			}, nil
		}),
		"secret",
	)
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestMutationFailureIsNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	expected := errors.New("connection reset")
	transport := newHeaderTransport(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, expected
		}),
		"",
	)
	request, _ := http.NewRequest(http.MethodPost, "https://example.test", strings.NewReader("{}"))
	_, err := transport.RoundTrip(request)
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("mutation calls = %d", calls.Load())
	}
}

func TestMetaIncludesRetryAfter(t *testing.T) {
	t.Parallel()

	reset := time.Now().Add(time.Minute).Unix()
	response := &http.Response{
		Header: http.Header{
			"Retry-After":         []string{"10"},
			"X-Ratelimit-Reset":   []string{strconv.FormatInt(reset, 10)},
			"X-Github-Request-Id": []string{"ABC0:1234"},
		},
	}
	meta := metaFromResponse(response)
	if meta.RequestID != "ABC0:1234" {
		t.Fatalf("request ID = %q", meta.RequestID)
	}
	if meta.Rate.ResetAt.Unix() != reset {
		t.Fatalf("reset = %s", meta.Rate.ResetAt)
	}
	if meta.Rate.RetryAt.Before(time.Now().Add(9*time.Second)) || meta.Rate.RetryAt.After(time.Now().Add(11*time.Second)) {
		t.Fatalf("retry at = %s", meta.Rate.RetryAt)
	}
}
