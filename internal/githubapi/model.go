package githubapi

import "time"

type Issue struct {
	Number    int64
	Title     string
	Body      string
	Labels    []string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	ETag      string
	IsPR      bool
}

type Comment struct {
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

type Meta struct {
	RequestID string
	ETag      string
	Rate      Rate
}

type IssuePage struct {
	Items      []Issue
	NextToken  string
	Meta       Meta
	Total      int
	Searched   bool
	Incomplete bool
	Truncated  bool
}

type CommentPage struct {
	Items     []Comment
	NextToken string
	Meta      Meta
}

type Error struct {
	Code      string
	Message   string
	Status    int
	RequestID string
	Rate      Rate
	Unknown   bool
	Cause     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }
