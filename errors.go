package gia

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeNotFound        ErrorCode = "not_found"
	CodeAuthentication  ErrorCode = "authentication"
	CodePermission      ErrorCode = "permission"
	CodeValidation      ErrorCode = "validation"
	CodeInvalidResource ErrorCode = "invalid_resource"
	CodeRateLimited     ErrorCode = "rate_limited"
	CodeNotModified     ErrorCode = "not_modified"
	CodeTransport       ErrorCode = "transport"
	CodeGitHub          ErrorCode = "github"
	CodeOutcomeUnknown  ErrorCode = "outcome_unknown"
)

type Outcome string

const (
	OutcomeNotApplied Outcome = "not_applied"
	OutcomeApplied    Outcome = "applied"
	OutcomeUnknown    Outcome = "unknown"
)

// Error is the stable cross-SDK error shape.
type Error struct {
	Code      ErrorCode
	Message   string
	Outcome   Outcome
	Status    int
	RequestID string
	Rate      Rate
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Status != 0 {
		return fmt.Sprintf("GiaDB: %s (HTTP %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("GiaDB: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func IsCode(err error, code ErrorCode) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

func invalidArgument(message string) error {
	return &Error{
		Code:    CodeInvalidArgument,
		Message: message,
		Outcome: OutcomeNotApplied,
	}
}
