package dataset

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBaseURL = "https://context-video/specs/"

const (
	manifestSchemaFile    = "golden-dataset-manifest-v1.schema.json"
	groundTruthSchemaFile = "golden-ground-truth-v1.schema.json"
)

var schemaFiles = []string{
	"common.schema.json",
	manifestSchemaFile,
	groundTruthSchemaFile,
}

// Loader reads and validates golden datasets from injected filesystems.
type Loader struct {
	datasetFS         fs.FS
	manifestSchema    *jsonschema.Schema
	groundTruthSchema *jsonschema.Schema
}

// NewLoader compiles the golden dataset schemas from specsFS.
func NewLoader(datasetFS, specsFS fs.FS) (*Loader, error) {
	if datasetFS == nil {
		return nil, fmt.Errorf("dataset fs is nil")
	}
	if specsFS == nil {
		return nil, fmt.Errorf("specs fs is nil")
	}

	compiler := jsonschema.NewCompiler()
	for _, name := range schemaFiles {
		data, err := fs.ReadFile(specsFS, name)
		if err != nil {
			return nil, fmt.Errorf("read schema %s: %w", name, err)
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", name, err)
		}
		if err := compiler.AddResource(schemaBaseURL+name, document); err != nil {
			return nil, fmt.Errorf("add schema %s: %w", name, err)
		}
	}

	manifestSchema, err := compiler.Compile(schemaBaseURL + manifestSchemaFile)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", manifestSchemaFile, err)
	}
	groundTruthSchema, err := compiler.Compile(schemaBaseURL + groundTruthSchemaFile)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", groundTruthSchemaFile, err)
	}
	return &Loader{
		datasetFS:         datasetFS,
		manifestSchema:    manifestSchema,
		groundTruthSchema: groundTruthSchema,
	}, nil
}

func validateSchema(schema *jsonschema.Schema, source string, data []byte) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return &Error{Layer: LayerSchema, Source: source, Message: err.Error()}
	}
	if err := schema.Validate(instance); err != nil {
		var validationError *jsonschema.ValidationError
		if errors.As(err, &validationError) {
			return &Error{
				Layer:   LayerSchema,
				Source:  source,
				Path:    firstInstancePath(validationError),
				Message: validationError.Error(),
			}
		}
		return &Error{Layer: LayerSchema, Source: source, Message: err.Error()}
	}
	return nil
}

func firstInstancePath(err *jsonschema.ValidationError) string {
	if err == nil {
		return ""
	}
	if len(err.InstanceLocation) > 0 {
		return "/" + strings.Join(err.InstanceLocation, "/")
	}
	for _, cause := range err.Causes {
		if location := firstInstancePath(cause); location != "" {
			return location
		}
	}
	return ""
}
