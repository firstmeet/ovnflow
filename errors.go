package ovnflow

import (
	"context"
	"errors"
	"fmt"
)

// ErrorKind classifies errors returned by ovnflow.
type ErrorKind string

const (
	ErrorAlreadyExists ErrorKind = "already_exists"
	ErrorNotFound      ErrorKind = "not_found"
	ErrorConflict      ErrorKind = "conflict"
	ErrorUnavailable   ErrorKind = "unavailable"
	ErrorInvalidSchema ErrorKind = "invalid_schema"
	ErrorTimeout       ErrorKind = "timeout"
	ErrorCanceled      ErrorKind = "canceled"
	ErrorPartial       ErrorKind = "partial_success"
	ErrorValidation    ErrorKind = "validation"
)

// Error is a typed error suitable for controller retry and branching logic.
type Error struct {
	Kind      ErrorKind
	Database  string
	Table     string
	Operation string
	Object    string
	Message   string
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := string(e.Kind)
	if e.Operation != "" {
		msg += " " + e.Operation
	}
	if e.Table != "" {
		msg += " " + e.Table
	}
	if e.Object != "" {
		msg += " " + e.Object
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	targetErr, ok := target.(*Error)
	return ok && e != nil && e.Kind == targetErr.Kind
}

var (
	ErrAlreadyExists = &Error{Kind: ErrorAlreadyExists}
	ErrNotFound      = &Error{Kind: ErrorNotFound}
	ErrConflict      = &Error{Kind: ErrorConflict}
	ErrUnavailable   = &Error{Kind: ErrorUnavailable}
	ErrInvalidSchema = &Error{Kind: ErrorInvalidSchema}
	ErrTimeout       = &Error{Kind: ErrorTimeout}
	ErrCanceled      = &Error{Kind: ErrorCanceled}
	ErrPartial       = &Error{Kind: ErrorPartial}
	ErrValidation    = &Error{Kind: ErrorValidation}
)

// IsKind reports whether err is an ovnflow error of kind.
func IsKind(err error, kind ErrorKind) bool {
	return KindOf(err) == kind
}

// KindOf returns the ovnflow error kind, if present.
func KindOf(err error) ErrorKind {
	var ovnErr *Error
	if errors.As(err, &ovnErr) {
		return ovnErr.Kind
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorTimeout
	default:
		return ""
	}
}

func wrap(kind ErrorKind, database, table, op, object, message string, err error) error {
	if err == nil && message == "" {
		message = fmt.Sprintf("%s failed", op)
	}
	return &Error{
		Kind:      kind,
		Database:  database,
		Table:     table,
		Operation: op,
		Object:    object,
		Message:   message,
		Err:       err,
	}
}

func classifyContext(err error, database, table, op, object string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return wrap(ErrorCanceled, database, table, op, object, "", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return wrap(ErrorTimeout, database, table, op, object, "", err)
	}
	return wrap(ErrorUnavailable, database, table, op, object, "", err)
}
