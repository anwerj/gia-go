package githubapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
)

type Config struct {
	Owner       string
	Repository  string
	Token       string
	HTTPClient  *http.Client
	RESTBaseURL string
	GraphQLURL  string
}

type Client struct {
	owner      string
	repository string
	rest       *github.Client
	http       *http.Client
	graphqlURL string
}

func New(config Config) (*Client, error) {
	baseClient := config.HTTPClient
	if baseClient == nil {
		baseClient = &http.Client{Timeout: 30 * time.Second}
	}
	httpClient := *baseClient
	httpClient.Transport = newHeaderTransport(baseClient.Transport, config.Token)

	clientOptions := []github.ClientOptionsFunc{
		github.WithHTTPClient(&httpClient),
		github.WithDisableRateLimitCheck(),
	}
	if config.RESTBaseURL != "" {
		base := strings.TrimRight(config.RESTBaseURL, "/") + "/"
		clientOptions = append(clientOptions, github.WithURLs(&base, nil))
	}
	rest, err := github.NewClient(clientOptions...)
	if err != nil {
		return nil, fmt.Errorf("create GitHub REST client: %w", err)
	}

	graphqlURL := config.GraphQLURL
	if graphqlURL == "" {
		graphqlURL = "https://api.github.com/graphql"
	}
	if _, err := http.NewRequest(http.MethodPost, graphqlURL, nil); err != nil {
		return nil, fmt.Errorf("parse GraphQL URL: %w", err)
	}

	return &Client{
		owner:      config.Owner,
		repository: config.Repository,
		rest:       rest,
		http:       &httpClient,
		graphqlURL: graphqlURL,
	}, nil
}

func apiError(err error, mutation bool) error {
	if err == nil {
		return nil
	}
	var primaryLimit *github.RateLimitError
	if errors.As(err, &primaryLimit) {
		meta := metaFromResponse(primaryLimit.Response)
		meta.Rate.Limit = primaryLimit.Rate.Limit
		meta.Rate.Remaining = primaryLimit.Rate.Remaining
		meta.Rate.ResetAt = primaryLimit.Rate.Reset.Time
		return &Error{
			Code:      "rate_limited",
			Message:   primaryLimit.Message,
			Status:    primaryLimit.Response.StatusCode,
			RequestID: meta.RequestID,
			Rate:      meta.Rate,
			Cause:     err,
		}
	}
	var secondaryLimit *github.AbuseRateLimitError
	if errors.As(err, &secondaryLimit) {
		meta := metaFromResponse(secondaryLimit.Response)
		if secondaryLimit.RetryAfter != nil {
			meta.Rate.RetryAt = time.Now().Add(*secondaryLimit.RetryAfter)
		}
		return &Error{
			Code:      "rate_limited",
			Message:   secondaryLimit.Message,
			Status:    secondaryLimit.Response.StatusCode,
			RequestID: meta.RequestID,
			Rate:      meta.Rate,
			Cause:     err,
		}
	}
	var responseError *github.ErrorResponse
	if errors.As(err, &responseError) {
		status := 0
		requestID := ""
		rate := Rate{}
		if responseError.Response != nil {
			status = responseError.Response.StatusCode
			meta := metaFromResponse(responseError.Response)
			requestID = meta.RequestID
			rate = meta.Rate
		}
		code := "github"
		switch status {
		case http.StatusNotModified:
			code = "not_modified"
		case http.StatusUnauthorized:
			code = "authentication"
		case http.StatusForbidden:
			if rate.Remaining == 0 || !rate.RetryAt.IsZero() {
				code = "rate_limited"
			} else {
				code = "permission"
			}
		case http.StatusNotFound:
			code = "not_found"
		case http.StatusUnprocessableEntity, http.StatusBadRequest:
			code = "validation"
		case http.StatusTooManyRequests:
			code = "rate_limited"
		}
		return &Error{
			Code:      code,
			Message:   responseError.Message,
			Status:    status,
			RequestID: requestID,
			Rate:      rate,
			Cause:     err,
		}
	}
	code := "transport"
	if mutation {
		code = "outcome_unknown"
	}
	return &Error{
		Code:    code,
		Message: err.Error(),
		Unknown: mutation,
		Cause:   err,
	}
}
