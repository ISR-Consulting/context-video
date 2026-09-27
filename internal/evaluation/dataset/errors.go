package dataset

import "strings"

const (
	LayerSchema    = "schema"
	LayerDomain    = "domain"
	LayerReference = "reference"
)

// Error is one layered dataset validation failure.
type Error struct {
	Layer   string
	Source  string
	Path    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{e.Layer}
	if e.Source != "" {
		parts = append(parts, e.Source)
	}
	if e.Path != "" {
		parts = append(parts, e.Path)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, ": ")
}

// ErrorList preserves validation failures in deterministic discovery order.
type ErrorList []Error

func (e ErrorList) Error() string {
	parts := make([]string, len(e))
	for i := range e {
		parts[i] = e[i].Error()
	}
	return strings.Join(parts, "; ")
}

// Err returns nil when no failures were collected.
func (e ErrorList) Err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}
