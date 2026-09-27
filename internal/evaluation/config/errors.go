package config

import "strings"

// Stage identifies where configuration loading failed.
type Stage string

const (
	StageRead           Stage = "read"
	StageDecode         Stage = "decode"
	StageDomain         Stage = "domain"
	StageSchemaSelector Stage = "schema-selector"
)

// Issue is one semantic validation failure at a YAML path.
type Issue struct {
	Path    string
	Message string
}

// Error is a configuration loading failure. Issues are in deterministic
// discovery order. Err, when set, is the underlying cause.
type Error struct {
	Stage  Stage
	Source string
	Issues []Issue
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	prefix := "config " + string(e.Stage)
	if e.Source != "" {
		prefix += ": " + e.Source
	}
	var details []string
	if e.Err != nil {
		details = append(details, e.Err.Error())
	}
	for _, issue := range e.Issues {
		if issue.Path != "" {
			details = append(details, issue.Path+": "+issue.Message)
		} else {
			details = append(details, issue.Message)
		}
	}
	if len(details) == 0 {
		return prefix
	}
	return prefix + ": " + strings.Join(details, "; ")
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
