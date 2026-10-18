# gia-go

`gia-go` is the Go 1.26 reference SDK for GiaDB v0.1.

```go
client, err := gia.NewClient(gia.Options{
    Owner:      "octocat",
    Repository: "knowledge",
    Token:      os.Getenv("GITHUB_TOKEN"),
})
if err != nil {
    log.Fatal(err)
}

resource, _, err := client.Create(ctx, gia.CreateInput{
    Title: "Concise technical tone",
    Body:  "Prefer direct language.",
    Table: "tone",
})
```

The SDK uses GitHub REST for issue reads, writes, comments, and search. It uses
a fixed GraphQL query for issue-only repository enumeration. GitHub remains the
source of truth; no local persistence or cache is required.

Important behavior:

- Resource IDs are repository-scoped issue numbers.
- Every resource has exactly one `gia/table/<name>` label.
- `Get` does not load comments. Use paginated `ListSections`.
- Paged methods return opaque continuation tokens.
- `List` is the only resource collection/query method. It supports IDs, title
  matching, state, table, labels, timestamps, sorting, and pagination.
- Search-backed `List` calls expose incomplete and 1,000-result truncation
  indicators.
- The SDK does not retry, pace, or limit concurrency. Throttled responses are returned as `rate_limited` with the GitHub rate-limit details.
- An ambiguous mutation transport failure is `outcome_unknown`.
- Shared issue field updates are last-write-wins.

Callers may supply an authenticated `http.Client` instead of `Token`. GitHub
App installation-token acquisition is intentionally outside the SDK.
