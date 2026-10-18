//go:build live

package gia

import (
	"context"
	"os"
	"testing"
)

// Run with:
// go test -tags=live -run TestLiveList
func TestLiveList(t *testing.T) {
	token := os.Getenv("GIADB_TEST_TOKEN")
	owner := os.Getenv("GIADB_TEST_OWNER")
	repository := os.Getenv("GIADB_TEST_REPOSITORY")
	if token == "" || owner == "" || repository == "" {
		t.Skip("GIADB_TEST_TOKEN, GIADB_TEST_OWNER, and GIADB_TEST_REPOSITORY are required")
	}
	client, err := NewClient(Options{
		Owner:      owner,
		Repository: repository,
		Token:      token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List(context.Background(), ListOptions{PageOptions: PageOptions{PageSize: 10}}); err != nil {
		t.Fatal(err)
	}
}
