package files

import (
	"errors"
	"fmt"
	"io/fs"
)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func mapOSError(err error, context string) *Error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return NewError("not_found", "%s: %v", context, err)
	case errors.Is(err, fs.ErrPermission):
		return NewError("permission_denied", "%s: %v", context, err)
	default:
		return NewError("io_error", "%s: %v", context, err)
	}
}
