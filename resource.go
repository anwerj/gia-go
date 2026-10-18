package gia

import "time"

// State is the logical lifecycle state of a GiaDB resource.
type State string

const (
	StateActive    State = "active"
	StateForgotten State = "forgotten"
)

// Resource is a GitHub issue classified by exactly one gia/table label.
// Sections are intentionally loaded separately.
type Resource struct {
	ID        int64
	Title     string
	Body      string
	Table     string
	Labels    []string
	State     State
	CreatedAt time.Time
	UpdatedAt time.Time
	ETag      string
}

// Section is one first-class issue comment.
type Section struct {
	ID        int64
	Body      string
	Author    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Rate struct {
	Limit     int
	Remaining int
	ResetAt   time.Time
	RetryAt   time.Time
}

type ResponseMeta struct {
	RequestID string
	ETag      string
	Rate      Rate
}

type ResourcePage struct {
	Items         []Resource
	NextToken     string
	Exhausted     bool
	Meta          ResponseMeta
	ReportedTotal *int
	Incomplete    bool
	Truncated     bool
}

type SectionPage struct {
	Items     []Section
	NextToken string
	Exhausted bool
	Meta      ResponseMeta
}
