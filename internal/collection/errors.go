package collection

import (
	"context"
	"errors"
)

type Code string

const (
	CodeAuthentication Code = "authentication_required"
	CodeChallenge      Code = "challenge_required"
	CodePermission     Code = "permission_denied"
	CodeSchema         Code = "source_schema_invalid"
	CodeIncomplete     Code = "incomplete_scope"
	CodeNetwork        Code = "network_error"
	CodeRateLimit      Code = "rate_limited"
	CodeUnavailable    Code = "source_unavailable"
	CodeUnsupported    Code = "unsupported_connector"
	CodeCancelled      Code = "collection_cancelled"
)

// Failure intentionally retains no supplier error, response body, URL,
// credential, or session state. Its text and code are safe to persist or log.
type Failure struct {
	code Code
}

func NewFailure(code Code) *Failure {
	switch code {
	case CodeAuthentication, CodeChallenge, CodePermission, CodeSchema, CodeIncomplete,
		CodeNetwork, CodeRateLimit, CodeUnavailable, CodeUnsupported, CodeCancelled:
	default:
		code = CodeSchema
	}
	return &Failure{code: code}
}

func (f *Failure) Error() string { return "collection: " + string(f.code) }

// FailureCode normalizes even unclassified adapter errors without exposing
// their text. Callers should never persist or log a raw adapter error.
func FailureCode(err error) Code {
	if err == nil {
		return ""
	}
	var failure *Failure
	if errors.As(err, &failure) && failure != nil {
		return NewFailure(failure.code).code
	}
	if errors.Is(err, context.Canceled) {
		return CodeCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeNetwork
	}
	return CodeUnavailable
}

func Retryable(err error) bool {
	switch FailureCode(err) {
	case CodeNetwork, CodeRateLimit, CodeUnavailable:
		return true
	default:
		return false
	}
}

func safeFailure(err error) *Failure { return NewFailure(FailureCode(err)) }
