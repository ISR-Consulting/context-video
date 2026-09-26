package contracts

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBaseURL = "https://context-video/specs/"

// Kind selects which instance schema ValidateJSON applies.
type Kind string

const (
	KindAudioObservation  Kind = "audio-observation"
	KindVisualObservation Kind = "visual-observation"
	KindContextEventV1    Kind = "context-event-v1"
	KindMediaSegment      Kind = "media-segment"
	KindExperimentResult  Kind = "experiment-result"
)

// schemaFiles are loaded from the specs directory and compiled.
// common.schema.json is a $defs library, not an instance schema.
var schemaFiles = []string{
	"common.schema.json",
	"audio-observation.schema.json",
	"visual-observation.schema.json",
	"context-event-v1.schema.json",
	"media-segment.schema.json",
	"experiment-result.schema.json",
}

var kindSchema = map[Kind]string{
	KindAudioObservation:  "audio-observation.schema.json",
	KindVisualObservation: "visual-observation.schema.json",
	KindContextEventV1:    "context-event-v1.schema.json",
	KindMediaSegment:      "media-segment.schema.json",
	KindExperimentResult:  "experiment-result.schema.json",
}

// Validator validates raw JSON documents against the Context Video schemas.
// A Validator is immutable after NewValidator returns and is safe for concurrent use.
type Validator struct {
	schemas map[Kind]*jsonschema.Schema
}

// NewValidator loads every schema file from fsys.
// fsys is the specs directory: schema files live at its root, not under a nested specs/ folder.
// Resources are registered at their declared $id so relative $ref values resolve.
// Format assertions, including date-time, are enabled.
func NewValidator(fsys fs.FS) (*Validator, error) {
	if fsys == nil {
		return nil, fmt.Errorf("schema fs is nil")
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for _, name := range schemaFiles {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", name, err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", name, err)
		}
		url := schemaBaseURL + name
		if err := compiler.AddResource(url, doc); err != nil {
			return nil, fmt.Errorf("add schema %s: %w", name, err)
		}
	}

	fileKind := make(map[string]Kind, len(kindSchema))
	for kind, name := range kindSchema {
		fileKind[name] = kind
	}
	compiled := make(map[Kind]*jsonschema.Schema, len(kindSchema))
	for _, name := range schemaFiles {
		schema, err := compiler.Compile(schemaBaseURL + name)
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", name, err)
		}
		if kind, ok := fileKind[name]; ok {
			compiled[kind] = schema
		}
	}
	return &Validator{schemas: compiled}, nil
}

// ValidateJSON checks data against the schema for kind.
func (v *Validator) ValidateJSON(kind Kind, data []byte) error {
	if v == nil {
		return &Error{Layer: "schema", Message: "nil validator"}
	}
	schema, ok := v.schemas[kind]
	if !ok {
		return &Error{Layer: "schema", Message: fmt.Sprintf("unknown contract kind %q", kind)}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return &Error{Layer: "schema", Message: err.Error()}
	}
	if err := schema.Validate(inst); err != nil {
		return wrapSchemaError(err)
	}
	return nil
}

func wrapSchemaError(err error) error {
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return &Error{
			Layer:   "schema",
			Path:    firstInstancePath(ve),
			Message: ve.Error(),
		}
	}
	return &Error{Layer: "schema", Message: err.Error()}
}

func firstInstancePath(err *jsonschema.ValidationError) string {
	if err == nil {
		return ""
	}
	if len(err.InstanceLocation) > 0 {
		return "/" + strings.Join(err.InstanceLocation, "/")
	}
	for _, cause := range err.Causes {
		if path := firstInstancePath(cause); path != "" {
			return path
		}
	}
	return ""
}
