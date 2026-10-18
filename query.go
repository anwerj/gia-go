package gia

import "time"

// StateFilter controls list lifecycle selection.
type StateFilter string

const (
	StateFilterDefault   StateFilter = ""
	StateFilterActive    StateFilter = "active"
	StateFilterForgotten StateFilter = "forgotten"
	StateFilterAll       StateFilter = "all"
)

type SortField string

const (
	SortCreated SortField = "created"
	SortUpdated SortField = "updated"
)

type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

type CreateInput struct {
	Title  string
	Body   string
	Table  string
	Labels []string
}

// UpdateInput is a patch. Nil fields are not changed. Table alone preserves
// non-table labels. Labels alone replaces the complete label set. When both
// are present, Labels defines the non-table labels and any table label it
// contains must agree with Table.
type UpdateInput struct {
	Title  *string
	Body   *string
	Table  *string
	Labels *[]string
}

type GetOptions struct {
	IfNoneMatch string
}

type PageOptions struct {
	PageSize  int
	PageToken string
}

type ListOptions struct {
	PageOptions
	IDs          []int64
	TitleMatch   string
	CreatedSince *time.Time
	UpdatedSince *time.Time
	State        StateFilter
	Table        string
	AllLabels    []string
	Sort         SortField
	Direction    SortDirection
}
