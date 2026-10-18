package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anwerj/gia-go"
	"github.com/therewardstore/httpmatter"
)

func TestMain(m *testing.M) {
	if err := httpmatter.Init(&httpmatter.Config{
		BaseDir:       filepath.Join("testdata", "http"),
		FileExtension: ".http",
	}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestVendoredContractChecksums(t *testing.T) {
	root := filepath.Join("testdata", "contract", "v0.1")
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `json:"version"`
		Files   []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "0.1.0" {
		t.Fatalf("contract version = %q", manifest.Version)
	}
	for _, file := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(root, file.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != file.SHA256 {
			t.Errorf("%s checksum = %s, want %s", file.Path, got, file.SHA256)
		}
	}
}

func TestConformance(t *testing.T) {
	scenarios := loadScenarios(t)
	ran := map[string]bool{}
	run := func(id string, fn func(*testing.T)) {
		t.Helper()
		t.Run(id, func(t *testing.T) {
			ran[id] = true
			fn(t)
		})
	}

	run("labels.valid", func(t *testing.T) {
		useFixtures(t, "labels_valid", "get_issue", "get_issue_200")
		resource, _, err := testClient(t).Get(context.Background(), 1, gia.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if resource.Table != "skill" {
			t.Fatalf("table = %q", resource.Table)
		}
	})

	run("labels.ordinary_issue", func(t *testing.T) {
		useFixtures(t, "list_ordinary", "list_issues", "list_issues_200")
		page, err := testClient(t).List(context.Background(), gia.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("ordinary issue listed: %+v", page.Items)
		}
	})

	run("labels.multiple_tables", func(t *testing.T) {
		_, _, err := testClient(t).Create(context.Background(), gia.CreateInput{
			Title:  "Example",
			Table:  "skill",
			Labels: []string{"gia/table/skill", "gia/table/task"},
		})
		if !gia.IsCode(err, gia.CodeInvalidResource) {
			t.Fatalf("expected invalid_resource, got %v", err)
		}
	})

	run("labels.invalid_table", func(t *testing.T) {
		_, _, err := testClient(t).Create(context.Background(), gia.CreateInput{
			Title:  "Example",
			Table:  "skill",
			Labels: []string{"gia/table/Bad Name"},
		})
		if !gia.IsCode(err, gia.CodeInvalidResource) {
			t.Fatalf("expected invalid_resource, got %v", err)
		}
	})

	run("resource.table_matches_labels", func(t *testing.T) {
		resource := gia.Resource{
			Table:  "skill",
			Labels: []string{"gia/table/skill", "docs"},
		}
		if !resourceTableMatchesLabels(resource) {
			t.Fatalf("resource relation rejected: %+v", resource)
		}
	})

	run("resource.table_mismatch", func(t *testing.T) {
		resource := gia.Resource{
			Table:  "task",
			Labels: []string{"gia/table/skill", "docs"},
		}
		if resourceTableMatchesLabels(resource) {
			t.Fatalf("resource relation accepted: %+v", resource)
		}
	})

	run("list.pull_request", func(t *testing.T) {
		useFixtures(t, "list_pull_request", "get_issue", "get_issue_200")
		page, err := testClient(t).List(context.Background(), gia.ListOptions{IDs: []int64{7}})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("pull request listed: %+v", page.Items)
		}
	})

	run("list.empty_filtered_page_continues", func(t *testing.T) {
		useFixtures(t, "list_continue", "list_issues", "list_issues_200")
		page, err := testClient(t).List(context.Background(), gia.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 || page.Exhausted || page.NextToken == "" {
			t.Fatalf("page = %+v", page)
		}
	})

	run("list.state_default", func(t *testing.T) {
		page, err := stateFilterClient(t).List(
			context.Background(),
			gia.ListOptions{IDs: []int64{41, 42}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != 41 {
			t.Fatalf("items = %+v", page.Items)
		}
	})

	run("list.state_active", func(t *testing.T) {
		page, err := stateFilterClient(t).List(
			context.Background(),
			gia.ListOptions{
				IDs:   []int64{41, 42},
				State: gia.StateFilterActive,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != 41 {
			t.Fatalf("items = %+v", page.Items)
		}
	})

	run("list.state_forgotten", func(t *testing.T) {
		page, err := stateFilterClient(t).List(
			context.Background(),
			gia.ListOptions{
				IDs:   []int64{41, 42},
				State: gia.StateFilterForgotten,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != 42 {
			t.Fatalf("items = %+v", page.Items)
		}
	})

	run("sections.are_separate", func(t *testing.T) {
		if _, ok := reflect.TypeOf(gia.Resource{}).FieldByName("Sections"); ok {
			t.Fatal("resource includes sections")
		}
		useFixtures(t, "sections", "get_issue", "get_issue_200")
		resource, _, err := testClient(t).Get(context.Background(), 12, gia.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if resource.ID != 12 || resource.Body != "Primary content" {
			t.Fatalf("resource = %+v", resource)
		}
	})

	run("list.title_match_truncated", func(t *testing.T) {
		useFixtures(t, "list_truncated", "search_issues", "search_issues_200")
		page, err := testClient(t).List(context.Background(), gia.ListOptions{TitleMatch: "example"})
		if err != nil {
			t.Fatal(err)
		}
		if !page.Truncated || page.Incomplete || page.ReportedTotal == nil || *page.ReportedTotal <= 1000 {
			t.Fatalf("search metadata = %+v", page)
		}
	})

	run("list.title_match_incomplete", func(t *testing.T) {
		useFixtures(t, "list_incomplete", "search_issues", "search_issues_200")
		page, err := testClient(t).List(context.Background(), gia.ListOptions{TitleMatch: "example"})
		if err != nil {
			t.Fatal(err)
		}
		if !page.Incomplete || page.Truncated {
			t.Fatalf("search metadata = %+v", page)
		}
	})

	run("update.preserve_labels", func(t *testing.T) {
		useFixtures(t, "update_labels",
			"get_issue", "get_issue_200",
			"patch_issue", "patch_issue_200",
		)
		table := "task"
		resource, _, err := testClient(t).Update(context.Background(), 21, gia.UpdateInput{Table: &table})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"gia/table/task", "docs", "custom"}
		if !sameLabels(resource.Labels, want) {
			t.Fatalf("labels = %v, want set %v", resource.Labels, want)
		}
	})

	run("update.replace_labels", func(t *testing.T) {
		useFixtures(t, "update_replace_labels", "patch_issue", "patch_issue_200")
		labels := []string{"gia/table/skill", "personal"}
		resource, _, err := testClient(t).Update(
			context.Background(),
			22,
			gia.UpdateInput{Labels: &labels},
		)
		if err != nil {
			t.Fatal(err)
		}
		if !sameLabels(resource.Labels, labels) {
			t.Fatalf("labels = %v, want set %v", resource.Labels, labels)
		}
	})

	run("update.table_with_labels", func(t *testing.T) {
		useFixtures(t, "update_table_with_labels", "patch_issue", "patch_issue_200")
		table := "task"
		labels := []string{"personal"}
		resource, _, err := testClient(t).Update(
			context.Background(),
			23,
			gia.UpdateInput{Table: &table, Labels: &labels},
		)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"gia/table/task", "personal"}
		if !sameLabels(resource.Labels, want) {
			t.Fatalf("labels = %v, want set %v", resource.Labels, want)
		}
	})

	run("update.conflicting_table", func(t *testing.T) {
		table := "task"
		labels := []string{"gia/table/process", "personal"}
		_, _, err := testClient(t).Update(
			context.Background(),
			24,
			gia.UpdateInput{Table: &table, Labels: &labels},
		)
		if !gia.IsCode(err, gia.CodeInvalidArgument) {
			t.Fatalf("expected invalid_argument, got %v", err)
		}
	})

	run("lifecycle.forget", func(t *testing.T) {
		useFixtures(t, "lifecycle_forget", "patch_issue", "patch_issue_200")
		resource, _, err := testClient(t).Forget(context.Background(), 31)
		if err != nil {
			t.Fatal(err)
		}
		if resource.State != gia.StateForgotten {
			t.Fatalf("state = %q", resource.State)
		}
	})

	run("lifecycle.reopen", func(t *testing.T) {
		useFixtures(t, "lifecycle_reopen", "patch_issue", "patch_issue_200")
		resource, _, err := testClient(t).Reopen(context.Background(), 32)
		if err != nil {
			t.Fatal(err)
		}
		if resource.State != gia.StateActive {
			t.Fatalf("state = %q", resource.State)
		}
	})

	run("throttle.retry_after_precedence", func(t *testing.T) {
		var calls int
		var slept time.Duration
		reset := strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10)
		client, err := gia.NewClient(gia.Options{
			Owner:      "acme",
			Repository: "db",
			HTTPClient: &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Header: http.Header{
							"Retry-After":           []string{"10"},
							"X-Ratelimit-Remaining": []string{"0"},
							"X-Ratelimit-Reset":     []string{reset},
						},
						Body:    io.NopCloser(stringsReader(`{"message":"slow down"}`)),
						Request: request,
					}, nil
				}
				return issueResponse(request, 4, "open", []string{"gia/table/task"}), nil
			})},
			MutationInterval: time.Nanosecond,
			Sleep: func(_ context.Context, delay time.Duration) error {
				slept = delay
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := client.Get(context.Background(), 4, gia.GetOptions{}); err != nil {
			t.Fatal(err)
		}
		if slept < 10*time.Second || slept > 12*time.Second {
			t.Fatalf("retry delay = %s, want Retry-After 10s rather than the 60s reset", slept)
		}
	})

	run("mutation.ambiguous_transport_failure", func(t *testing.T) {
		var calls int
		client, err := gia.NewClient(gia.Options{
			Owner:      "acme",
			Repository: "db",
			HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("connection reset")
			})},
			MaxReadRetries:   -1,
			MutationInterval: time.Nanosecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = client.Create(context.Background(), gia.CreateInput{Title: "x", Table: "task"})
		if !gia.IsCode(err, gia.CodeOutcomeUnknown) {
			t.Fatalf("expected outcome_unknown, got %v", err)
		}
		if calls != 1 {
			t.Fatalf("mutation calls = %d", calls)
		}
	})

	run("conditional.not_modified", func(t *testing.T) {
		useFixtures(t, "conditional", "get_issue", "get_issue_304")
		_, _, err := testClient(t).Get(context.Background(), 3, gia.GetOptions{IfNoneMatch: `"known"`})
		if !gia.IsCode(err, gia.CodeNotModified) {
			t.Fatalf("expected not_modified, got %v", err)
		}
	})

	for _, id := range scenarios {
		if !ran[id] {
			t.Errorf("scenario %s was not run", id)
		}
	}
}

func TestListMissingIDIsNotFound(t *testing.T) {
	useFixtures(t, "missing_id", "get_issue", "get_issue_404")
	_, err := testClient(t).List(context.Background(), gia.ListOptions{IDs: []int64{5}})
	if !gia.IsCode(err, gia.CodeNotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestListByIDReturnsForgottenResourceWhenRequested(t *testing.T) {
	useFixtures(t, "forgotten_id", "get_issue", "get_issue_200")
	page, err := testClient(t).List(
		context.Background(),
		gia.ListOptions{
			IDs:   []int64{9},
			State: gia.StateFilterForgotten,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 9 || page.Items[0].State != gia.StateForgotten {
		t.Fatalf("page = %+v", page)
	}
}

func TestListByIDDefaultsToActive(t *testing.T) {
	useFixtures(t, "forgotten_id", "get_issue", "get_issue_200")
	page, err := testClient(t).List(
		context.Background(),
		gia.ListOptions{IDs: []int64{9}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("forgotten resource returned by active-default list: %+v", page.Items)
	}
}

func loadScenarios(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", "v0.1", "conformance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(contract.Scenarios))
	for i, scenario := range contract.Scenarios {
		ids[i] = scenario.ID
	}
	return ids
}

func resourceTableMatchesLabels(resource gia.Resource) bool {
	tableLabel := "gia/table/" + resource.Table
	count := 0
	for _, label := range resource.Labels {
		if strings.HasPrefix(label, "gia/table/") {
			count++
			if label != tableLabel {
				return false
			}
		}
	}
	return count == 1
}

func stateFilterClient(t *testing.T) *gia.Client {
	t.Helper()
	client, err := gia.NewClient(gia.Options{
		Owner:      "acme",
		Repository: "db",
		HTTPClient: &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
			switch {
			case strings.HasSuffix(request.URL.Path, "/41"):
				return issueResponse(request, 41, "open", []string{"gia/table/task"}), nil
			case strings.HasSuffix(request.URL.Path, "/42"):
				return issueResponse(request, 42, "closed", []string{"gia/table/task"}), nil
			default:
				return nil, errors.New("unexpected resource ID")
			}
		})},
		MaxReadRetries:   -1,
		MutationInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func useFixtures(t *testing.T, namespace string, names ...string) {
	t.Helper()
	if len(names)%2 != 0 {
		t.Fatal("fixture names must be request/response pairs")
	}
	mock := httpmatter.NewHTTP(t, namespace)
	for i := 0; i < len(names); i += 2 {
		mock = mock.Add(names[i], names[i+1]).Respond(matchFixture(t))
	}
	mock.Init()
	t.Cleanup(mock.Destroy)
}

func matchFixture(t *testing.T) func(*http.Request, *httpmatter.RequestMatter, []*httpmatter.ResponseMatter) *httpmatter.ResponseMatter {
	t.Helper()
	return func(req *http.Request, reqm *httpmatter.RequestMatter, respms []*httpmatter.ResponseMatter) *httpmatter.ResponseMatter {
		t.Helper()
		for key, want := range reqm.Header {
			if key == "Content-Length" || key == "User-Agent" {
				continue
			}
			if got := req.Header.Values(key); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s header %s = %q, fixture %q", req.Method, req.URL, key, got, want)
			}
		}
		gotBody := readBody(t, req.Body)
		wantBody, err := reqm.BodyBytes()
		if err != nil {
			t.Fatal(err)
		}
		if !jsonEqual(gotBody, wantBody) {
			t.Errorf("%s %s body = %s, fixture %s", req.Method, req.URL, gotBody, wantBody)
		}
		return respms[0]
	}
}

func readBody(t *testing.T, body io.ReadCloser) []byte {
	t.Helper()
	if body == nil {
		return nil
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jsonEqual(got, want []byte) bool {
	if len(bytes.TrimSpace(got)) == 0 && len(bytes.TrimSpace(want)) == 0 {
		return true
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		return false
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		return false
	}
	return reflect.DeepEqual(gotValue, wantValue)
}

func sameLabels(got, want []string) bool {
	left := slices.Clone(got)
	right := slices.Clone(want)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func testClient(t *testing.T) *gia.Client {
	t.Helper()
	client, err := gia.NewClient(gia.Options{
		Owner:            "acme",
		Repository:       "db",
		MaxReadRetries:   -1,
		MutationInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func stringsReader(body string) io.Reader {
	return bytes.NewReader([]byte(body))
}

func issueResponse(request *http.Request, number int, state string, labels []string) *http.Response {
	payload, err := json.Marshal(map[string]any{
		"number":     number,
		"title":      "resource",
		"body":       "body",
		"state":      state,
		"labels":     labelObjects(labels),
		"created_at": "2026-01-02T03:04:05Z",
		"updated_at": "2026-01-02T03:04:05Z",
	})
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(payload)),
		Request:    request,
	}
}

func labelObjects(labels []string) []map[string]string {
	objects := make([]map[string]string, len(labels))
	for i, label := range labels {
		objects[i] = map[string]string{"name": label}
	}
	return objects
}
