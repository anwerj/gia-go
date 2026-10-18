package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/anwerj/gia-go"
	"github.com/therewardstore/httpmatter"
)

func TestMain(m *testing.M) {
	if err := httpmatter.Init(&httpmatter.Config{
		BaseDir:       "testdata",
		FileExtension: ".http",
	}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestCreateSendsOneIssueRequest(t *testing.T) {
	useFixtures(t, "create", "post_issue", "post_issue_201")

	resource, _, err := testClient(t).Create(context.Background(), gia.CreateInput{
		Title:  "Example",
		Body:   "Body",
		Table:  "skill",
		Labels: []string{"docs"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resource.ID != 17 || resource.Table != "skill" {
		t.Fatalf("resource = %+v", resource)
	}
}

func TestGraphQLListFiltersOrdinaryIssues(t *testing.T) {
	useFixtures(t, "list_graphql", "list_issues", "list_issues_200")

	page, err := testClient(t).List(context.Background(), gia.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 2 {
		t.Fatalf("items = %+v", page.Items)
	}
	if page.Exhausted || page.NextToken == "" {
		t.Fatalf("continuation was lost: %+v", page)
	}
}

func TestConditionalGetNotModified(t *testing.T) {
	useFixtures(t, "get_not_modified", "get_issue", "get_issue_304")

	_, _, err := testClient(t).Get(context.Background(), 3, gia.GetOptions{IfNoneMatch: `"known"`})
	if !gia.IsCode(err, gia.CodeNotModified) {
		t.Fatalf("expected not_modified, got %v", err)
	}
}

func TestListTitleMatchIsIssueOnlyAndReportsTruncation(t *testing.T) {
	useFixtures(t, "list_search", "search_issues", "search_issues_200")

	page, err := testClient(t).List(context.Background(), gia.ListOptions{TitleMatch: "example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 9 {
		t.Fatalf("items = %+v", page.Items)
	}
	if !page.Incomplete || !page.Truncated || page.ReportedTotal == nil || *page.ReportedTotal != 1200 {
		t.Fatalf("search metadata = %+v", page)
	}
}

func TestListByIDsPaginates(t *testing.T) {
	useFixtures(t, "list_by_ids",
		"get_issue_7", "get_issue_7_200",
		"get_issue_9", "get_issue_9_200",
	)

	client := testClient(t)
	options := gia.ListOptions{
		IDs:         []int64{7, 9},
		PageOptions: gia.PageOptions{PageSize: 1},
	}
	first, err := client.List(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != 7 || first.Exhausted {
		t.Fatalf("first page = %+v", first)
	}
	options.PageToken = first.NextToken
	second, err := client.List(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != 9 || !second.Exhausted {
		t.Fatalf("second page = %+v", second)
	}
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

func matchFixture(t *testing.T) func(req *http.Request, reqm *httpmatter.RequestMatter, respms []*httpmatter.ResponseMatter) *httpmatter.ResponseMatter {
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

func testClient(t *testing.T) *gia.Client {
	t.Helper()
	client, err := gia.NewClient(gia.Options{
		Owner:      "acme",
		Repository: "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
