package githubapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"
)

type ListMatchInput struct {
	PageSize     int
	PageToken    string
	Text         string
	In           string
	CreatedSince *time.Time
	UpdatedSince *time.Time
	State        string
	Table        string
	Labels       []string
	Sort         string
	Direction    string
}

func (c *Client) Create(ctx context.Context, title, body string, labels []string) (Issue, Meta, error) {
	request := github.CreateIssueRequest{Title: title, Body: &body, Labels: labels}
	issue, response, err := c.rest.Issues.Create(ctx, c.owner, c.repository, request)
	meta := responseMeta(response)
	if err != nil {
		return Issue{}, meta, apiError(err, true)
	}
	return issueFromGitHub(issue, meta.ETag), meta, nil
}

func (c *Client) Get(ctx context.Context, number int64, ifNoneMatch string) (Issue, Meta, error) {
	path := fmt.Sprintf(
		"repos/%s/%s/issues/%d",
		url.PathEscape(c.owner),
		url.PathEscape(c.repository),
		number,
	)
	request, err := c.rest.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Issue{}, Meta{}, apiError(err, false)
	}
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	issue := new(github.Issue)
	response, err := c.rest.Do(request, issue)
	meta := responseMeta(response)
	if err != nil {
		return Issue{}, meta, apiError(err, false)
	}
	return issueFromGitHub(issue, meta.ETag), meta, nil
}

func (c *Client) Update(ctx context.Context, number int64, title, body *string, labels *[]string) (Issue, Meta, error) {
	request := github.UpdateIssueRequest{Title: title, Body: body}
	if labels != nil {
		request.Labels = *labels
	}
	issue, response, err := c.rest.Issues.Update(ctx, c.owner, c.repository, int(number), request)
	meta := responseMeta(response)
	if err != nil {
		return Issue{}, meta, apiError(err, true)
	}
	return issueFromGitHub(issue, meta.ETag), meta, nil
}

func (c *Client) SetState(ctx context.Context, number int64, state string) (Issue, Meta, error) {
	reason := "not_planned"
	if state == "open" {
		reason = "reopened"
	}
	request := github.UpdateIssueRequest{State: &state, StateReason: &reason}
	issue, response, err := c.rest.Issues.Update(ctx, c.owner, c.repository, int(number), request)
	meta := responseMeta(response)
	if err != nil {
		return Issue{}, meta, apiError(err, true)
	}
	return issueFromGitHub(issue, meta.ETag), meta, nil
}

func (c *Client) AppendComment(ctx context.Context, number int64, body string) (Comment, Meta, error) {
	request := github.IssueCommentRequest{Body: body}
	comment, response, err := c.rest.Issues.CreateComment(ctx, c.owner, c.repository, int(number), request)
	meta := responseMeta(response)
	if err != nil {
		return Comment{}, meta, apiError(err, true)
	}
	return commentFromGitHub(comment), meta, nil
}

func (c *Client) ListComments(ctx context.Context, number int64, pageSize int, token string) (CommentPage, error) {
	page, err := decodePageToken(token, "comments")
	if err != nil {
		return CommentPage{}, err
	}
	if pageSize == 0 {
		pageSize = 100
	}
	comments, response, err := c.rest.Issues.ListComments(
		ctx,
		c.owner,
		c.repository,
		int(number),
		&github.IssueListCommentsOptions{ListOptions: github.ListOptions{Page: page, PerPage: pageSize}},
	)
	if err != nil {
		return CommentPage{}, apiError(err, false)
	}
	items := make([]Comment, 0, len(comments))
	for _, comment := range comments {
		items = append(items, commentFromGitHub(comment))
	}
	next := ""
	if response.NextPage != 0 {
		next = encodePageToken("comments", response.NextPage)
	}
	return CommentPage{Items: items, NextToken: next, Meta: responseMeta(response)}, nil
}

func (c *Client) ListByIDs(ctx context.Context, ids []int64, input ListInput) (IssuePage, error) {
	page, err := decodePageToken(input.PageToken, "ids")
	if err != nil {
		return IssuePage{}, err
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	start := (page - 1) * input.PageSize
	if start >= len(ids) {
		return IssuePage{}, nil
	}
	stop := min(start+input.PageSize, len(ids))
	items := make([]Issue, 0, stop-start)
	var meta Meta
	for _, id := range ids[start:stop] {
		issue, responseMeta, err := c.Get(ctx, id, "")
		meta = responseMeta
		if err != nil {
			return IssuePage{}, err
		}
		if issue.IsPR {
			continue
		}
		items = append(items, issue)
	}
	next := ""
	if stop < len(ids) {
		next = encodePageToken("ids", page+1)
	}
	return IssuePage{Items: items, NextToken: next, Meta: meta}, nil
}

func (c *Client) ListMatching(ctx context.Context, input ListMatchInput) (IssuePage, error) {
	page, err := decodePageToken(input.PageToken, "search")
	if err != nil {
		return IssuePage{}, err
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	query := buildSearchQuery(c.owner, c.repository, input)
	result, response, err := c.rest.Search.Issues(ctx, query, &github.SearchOptions{
		Sort:  input.Sort,
		Order: input.Direction,
		ListOptions: github.ListOptions{
			Page:    page,
			PerPage: input.PageSize,
		},
	})
	if err != nil {
		return IssuePage{}, apiError(err, false)
	}
	items := make([]Issue, 0, len(result.Issues))
	for _, issue := range result.Issues {
		converted := issueFromGitHub(issue, "")
		if converted.IsPR {
			continue
		}
		items = append(items, converted)
	}
	next := ""
	if response.NextPage != 0 && response.NextPage <= 10 {
		next = encodePageToken("search", response.NextPage)
	}
	total := result.GetTotal()
	return IssuePage{
		Items:      items,
		NextToken:  next,
		Meta:       responseMeta(response),
		Total:      total,
		Searched:   true,
		Incomplete: result.GetIncompleteResults(),
		Truncated:  total > 1000,
	}, nil
}

func buildSearchQuery(owner, repository string, input ListMatchInput) string {
	parts := []string{
		"repo:" + quoteQualifier(owner+"/"+repository),
		"is:issue",
	}
	if input.Text != "" {
		parts = append(parts, strconv.Quote(input.Text))
		if input.In != "" {
			parts = append(parts, "in:"+input.In)
		}
	}
	if input.UpdatedSince != nil {
		parts = append(parts, "updated:>="+input.UpdatedSince.UTC().Format(time.RFC3339))
	}
	if input.CreatedSince != nil {
		parts = append(parts, "created:>="+input.CreatedSince.UTC().Format(time.RFC3339))
	}
	switch input.State {
	case "active":
		parts = append(parts, "is:open")
	case "forgotten":
		parts = append(parts, "is:closed")
	}
	if input.Table != "" {
		parts = append(parts, "label:"+strconv.Quote("gia/table/"+input.Table))
	}
	for _, label := range input.Labels {
		parts = append(parts, "label:"+strconv.Quote(label))
	}
	return strings.Join(parts, " ")
}

func quoteQualifier(value string) string {
	if strings.ContainsAny(value, " \t\n\"") {
		return strconv.Quote(value)
	}
	return value
}

func issueFromGitHub(issue *github.Issue, etag string) Issue {
	if issue == nil {
		return Issue{}
	}
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, label.GetName())
	}
	result := Issue{
		Number: int64(issue.GetNumber()),
		Title:  issue.GetTitle(),
		Body:   issue.GetBody(),
		Labels: labels,
		State:  issue.GetState(),
		ETag:   etag,
		IsPR:   issue.PullRequestLinks != nil,
	}
	if issue.CreatedAt != nil {
		result.CreatedAt = issue.CreatedAt.Time
	}
	if issue.UpdatedAt != nil {
		result.UpdatedAt = issue.UpdatedAt.Time
	}
	return result
}

func commentFromGitHub(comment *github.IssueComment) Comment {
	if comment == nil {
		return Comment{}
	}
	result := Comment{
		ID:     comment.GetID(),
		Body:   comment.GetBody(),
		Author: comment.GetUser().GetLogin(),
	}
	if comment.CreatedAt != nil {
		result.CreatedAt = comment.CreatedAt.Time
	}
	if comment.UpdatedAt != nil {
		result.UpdatedAt = comment.UpdatedAt.Time
	}
	return result
}

func responseMeta(response *github.Response) Meta {
	if response == nil {
		return Meta{}
	}
	return metaFromResponse(response.Response)
}

func encodePageToken(kind string, page int) string {
	raw := kind + ":" + strconv.Itoa(page)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePageToken(token, kind string) (int, error) {
	if token == "" {
		return 1, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, &Error{Code: "invalid_argument", Message: "invalid page token", Cause: err}
	}
	prefix := kind + ":"
	if !strings.HasPrefix(string(decoded), prefix) {
		return 0, &Error{Code: "invalid_argument", Message: "page token belongs to another operation"}
	}
	page, err := strconv.Atoi(strings.TrimPrefix(string(decoded), prefix))
	if err != nil || page < 1 {
		return 0, &Error{Code: "invalid_argument", Message: "invalid page token"}
	}
	return page, nil
}
