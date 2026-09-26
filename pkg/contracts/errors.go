package contracts

import "strings"

// Error is one schema or domain validation failure.
type Error struct {
	Layer   string
	Path    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Path != "" && e.Message != "":
		return e.Layer + ": " + e.Path + ": " + e.Message
	case e.Message != "":
		return e.Layer + ": " + e.Message
	default:
		return e.Layer
	}
}

// ErrorList is a set of validation failures.
type ErrorList []Error

func (e ErrorList) Error() string {
	parts := make([]string, len(e))
	for i, err := range e {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "; ")
}

// Err returns nil when the list is empty so callers can return it directly.
func (e ErrorList) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}
