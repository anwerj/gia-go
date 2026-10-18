package githubapi

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiVersion = "2026-03-10"

type sleepFunc func(context.Context, time.Duration) error

type policyTransport struct {
	base             http.RoundTripper
	token            string
	maxReadRetries   int
	semaphore        chan struct{}
	mutationInterval time.Duration
	mutationMu       sync.Mutex
	nextMutation     time.Time
	now              func() time.Time
	sleep            sleepFunc
}

func newPolicyTransport(base http.RoundTripper, token string, concurrency, retries int, mutationInterval time.Duration, now func() time.Time, sleep sleepFunc) *policyTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if concurrency <= 0 {
		concurrency = 4
	}
	if retries < 0 {
		retries = 0
	}
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = sleepContext
	}
	return &policyTransport{
		base:             base,
		token:            token,
		maxReadRetries:   retries,
		semaphore:        make(chan struct{}, concurrency),
		mutationInterval: mutationInterval,
		now:              now,
		sleep:            sleep,
	}
}

func (t *policyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	safe := req.Method == http.MethodGet || req.Method == http.MethodHead || req.Header.Get("X-GIADB-Safe-Request") == "true"
	if !safe {
		if err := t.paceMutation(req.Context()); err != nil {
			return nil, err
		}
	}

	attempts := 1
	if safe {
		attempts += t.maxReadRetries
	}
	for attempt := 0; attempt < attempts; attempt++ {
		cloned := req.Clone(req.Context())
		cloned.Header = req.Header.Clone()
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			cloned.Body = body
		}
		cloned.Header.Del("X-GIADB-Safe-Request")
		cloned.Header.Set("Accept", "application/vnd.github+json")
		cloned.Header.Set("X-GitHub-Api-Version", apiVersion)
		cloned.Header.Set("User-Agent", "gia-go/0.1")
		if t.token != "" && cloned.Header.Get("Authorization") == "" {
			cloned.Header.Set("Authorization", "Bearer "+t.token)
		}

		if err := t.acquire(req.Context()); err != nil {
			return nil, err
		}
		resp, err := t.base.RoundTrip(cloned)
		t.release()
		if err != nil {
			if !safe || attempt+1 == attempts {
				return nil, err
			}
			if err := t.sleep(req.Context(), jitter(time.Second<<attempt)); err != nil {
				return nil, err
			}
			continue
		}

		delay, throttled := retryDelay(resp, t.now())
		if !safe || !throttled || attempt+1 == attempts {
			return resp, nil
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err := t.sleep(req.Context(), jitter(delay)); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("github transport: retry loop exhausted")
}

func (t *policyTransport) acquire(ctx context.Context) error {
	select {
	case t.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *policyTransport) release() { <-t.semaphore }

func (t *policyTransport) paceMutation(ctx context.Context) error {
	if t.mutationInterval <= 0 {
		return nil
	}
	t.mutationMu.Lock()
	defer t.mutationMu.Unlock()

	now := t.now()
	if now.Before(t.nextMutation) {
		if err := t.sleep(ctx, t.nextMutation.Sub(now)); err != nil {
			return err
		}
		now = t.now()
	}
	t.nextMutation = now.Add(t.mutationInterval)
	return nil
}

func retryDelay(resp *http.Response, now time.Time) (time.Duration, bool) {
	if resp == nil || (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusForbidden) {
		return 0, false
	}
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second, true
		}
		if at, err := http.ParseTime(value); err == nil {
			return max(at.Sub(now), 0), true
		}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if epoch, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return max(time.Unix(epoch, 0).Sub(now), 0), true
		}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return time.Minute, true
	}
	return 0, false
}

func jitter(value time.Duration) time.Duration {
	if value <= 0 {
		return 0
	}
	const spread = 0.2
	// Jitter is additive so a server-provided minimum is never violated.
	factor := 1 + rand.Float64()*spread
	return time.Duration(float64(value) * factor)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	if delay, ok := retryDelay(resp, time.Now()); ok {
		meta.Rate.RetryAt = time.Now().Add(delay)
	}
	return meta
}

func isMutation(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}
