package githubapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const listIssuesQuery = `
query GIADBListIssues(
  $owner: String!,
  $repository: String!,
  $first: Int!,
  $after: String,
  $states: [IssueState!],
  $labels: [String!],
  $orderBy: IssueOrder
) {
  repository(owner: $owner, name: $repository) {
    issues(first: $first, after: $after, states: $states, labels: $labels, orderBy: $orderBy) {
      nodes {
        number
        title
        body
        state
        createdAt
        updatedAt
        labels(first: 100) {
          nodes { name }
          pageInfo { hasNextPage }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
  rateLimit { limit remaining resetAt }
}`

type ListInput struct {
	PageSize  int
	PageToken string
	State     string
	Table     string
	Labels    []string
	Sort      string
	Direction string
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data struct {
		Repository *struct {
			Issues struct {
				Nodes []struct {
					Number    int64  `json:"number"`
					Title     string `json:"title"`
					Body      string `json:"body"`
					State     string `json:"state"`
					CreatedAt string `json:"createdAt"`
					UpdatedAt string `json:"updatedAt"`
					Labels    struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"labels"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		} `json:"repository"`
		RateLimit struct {
			Limit     int    `json:"limit"`
			Remaining int    `json:"remaining"`
			ResetAt   string `json:"resetAt"`
		} `json:"rateLimit"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"errors"`
}

func (c *Client) ListIssues(ctx context.Context, input ListInput) (IssuePage, error) {
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	cursor, err := decodeCursor(input.PageToken)
	if err != nil {
		return IssuePage{}, err
	}
	states := []string{"OPEN"}
	switch input.State {
	case "forgotten":
		states = []string{"CLOSED"}
	case "all":
		states = []string{"OPEN", "CLOSED"}
	}
	labels := append([]string(nil), input.Labels...)
	if input.Table != "" {
		labels = append(labels, "gia/table/"+input.Table)
	}
	field := "CREATED_AT"
	if input.Sort == "updated" {
		field = "UPDATED_AT"
	}
	direction := "DESC"
	if input.Direction == "asc" {
		direction = "ASC"
	}
	var after any
	if cursor != "" {
		after = cursor
	}
	payload, err := json.Marshal(graphQLRequest{
		Query: listIssuesQuery,
		Variables: map[string]any{
			"owner":      c.owner,
			"repository": c.repository,
			"first":      input.PageSize,
			"after":      after,
			"states":     states,
			"labels":     labels,
			"orderBy": map[string]string{
				"field":     field,
				"direction": direction,
			},
		},
	})
	if err != nil {
		return IssuePage{}, &Error{Code: "github", Message: "encode GraphQL request", Cause: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return IssuePage{}, &Error{Code: "transport", Message: err.Error(), Cause: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GIADB-Safe-Request", "true")
	response, err := c.http.Do(request)
	if err != nil {
		return IssuePage{}, apiError(err, false)
	}
	defer response.Body.Close()
	meta := metaFromResponse(response)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return IssuePage{}, &Error{Code: "transport", Message: "read GraphQL response", Cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return IssuePage{}, errorFromHTTP(response, body)
	}
	var decoded graphQLResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return IssuePage{}, &Error{Code: "github", Message: "decode GraphQL response", Cause: err}
	}
	if len(decoded.Errors) != 0 {
		messages := make([]string, 0, len(decoded.Errors))
		for _, item := range decoded.Errors {
			messages = append(messages, item.Message)
		}
		return IssuePage{}, &Error{
			Code:      graphQLErrorCode(decoded.Errors[0].Type),
			Message:   strings.Join(messages, "; "),
			Status:    response.StatusCode,
			RequestID: meta.RequestID,
			Rate:      meta.Rate,
		}
	}
	if decoded.Data.Repository == nil {
		return IssuePage{}, &Error{Code: "not_found", Message: "repository not found", Status: response.StatusCode}
	}

	connection := decoded.Data.Repository.Issues
	items := make([]Issue, 0, len(connection.Nodes))
	for _, node := range connection.Nodes {
		if node.Labels.PageInfo.HasNextPage {
			return IssuePage{}, &Error{
				Code:    "invalid_resource",
				Message: fmt.Sprintf("issue %d has more than 100 labels", node.Number),
			}
		}
		labels := make([]string, 0, len(node.Labels.Nodes))
		for _, label := range node.Labels.Nodes {
			labels = append(labels, label.Name)
		}
		createdAt, err := time.Parse(time.RFC3339, node.CreatedAt)
		if err != nil {
			return IssuePage{}, &Error{Code: "github", Message: "invalid createdAt from GraphQL", Cause: err}
		}
		updatedAt, err := time.Parse(time.RFC3339, node.UpdatedAt)
		if err != nil {
			return IssuePage{}, &Error{Code: "github", Message: "invalid updatedAt from GraphQL", Cause: err}
		}
		items = append(items, Issue{
			Number:    node.Number,
			Title:     node.Title,
			Body:      node.Body,
			Labels:    labels,
			State:     strings.ToLower(node.State),
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	next := ""
	if connection.PageInfo.HasNextPage {
		if connection.PageInfo.EndCursor == "" {
			return IssuePage{}, &Error{Code: "github", Message: "GraphQL page has no end cursor"}
		}
		next = encodeCursor(connection.PageInfo.EndCursor)
	}
	meta.Rate.Limit = decoded.Data.RateLimit.Limit
	meta.Rate.Remaining = decoded.Data.RateLimit.Remaining
	if reset, err := time.Parse(time.RFC3339, decoded.Data.RateLimit.ResetAt); err == nil {
		meta.Rate.ResetAt = reset
	}
	return IssuePage{Items: items, NextToken: next, Meta: meta}, nil
}

func encodeCursor(cursor string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("graphql:" + cursor))
}

func decodeCursor(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(decoded), "graphql:") {
		return "", &Error{Code: "invalid_argument", Message: "invalid list page token", Cause: err}
	}
	cursor := strings.TrimPrefix(string(decoded), "graphql:")
	if cursor == "" {
		return "", &Error{Code: "invalid_argument", Message: "invalid list page token"}
	}
	return cursor, nil
}

func graphQLErrorCode(kind string) string {
	switch strings.ToUpper(kind) {
	case "NOT_FOUND":
		return "not_found"
	case "FORBIDDEN":
		return "permission"
	case "UNAUTHENTICATED":
		return "authentication"
	case "RATE_LIMITED":
		return "rate_limited"
	default:
		return "github"
	}
}

func errorFromHTTP(response *http.Response, body []byte) error {
	meta := metaFromResponse(response)
	code := "github"
	switch response.StatusCode {
	case http.StatusUnauthorized:
		code = "authentication"
	case http.StatusForbidden:
		if meta.Rate.Remaining == 0 || !meta.Rate.RetryAt.IsZero() {
			code = "rate_limited"
		} else {
			code = "permission"
		}
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		code = "validation"
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = response.Status
	}
	return &Error{
		Code:      code,
		Message:   message,
		Status:    response.StatusCode,
		RequestID: meta.RequestID,
		Rate:      meta.Rate,
	}
}
