package githubapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSafeReadRetriesThrottle(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var slept time.Duration
	transport := newPolicyTransport(
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			call := calls.Add(1)
			if request.Header.Get("X-GitHub-Api-Version") != apiVersion {
				t.Errorf("missing API version header")
			}
			if call == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"2"}},
					Body:       io.NopCloser(strings.NewReader("limited")),
					Request:    request,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    request,
			}, nil
		}),
		"", 1, 1, 0, time.Now,
		func(_ context.Context, delay time.Duration) error {
			slept = delay
			return nil
		},
	)
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if slept < 2*time.Second || slept > 2400*time.Millisecond {
		t.Fatalf("retry delay = %s", slept)
	}
}

func TestMutationIsNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	expected := errors.New("connection reset")
	transport := newPolicyTransport(
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, expected
		}),
		"", 1, 3, 0, time.Now, nil,
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
