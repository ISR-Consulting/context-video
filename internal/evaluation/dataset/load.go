package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
)

// LoadManifest validates and decodes one manifest.
func (l *Loader) LoadManifest(name string) (Manifest, error) {
	var manifest Manifest
	if l == nil {
		return manifest, &Error{Layer: LayerReference, Source: name, Message: "nil loader"}
	}
	if err := validateDatasetPath(name); err != nil {
		return manifest, &Error{Layer: LayerReference, Source: name, Message: err.Error()}
	}
	data, err := fs.ReadFile(l.datasetFS, name)
	if err != nil {
		return manifest, &Error{Layer: LayerReference, Source: name, Message: fmt.Sprintf("read manifest: %v", err)}
	}
	if err := validateSchema(l.manifestSchema, name, data); err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, &Error{Layer: LayerSchema, Source: name, Message: err.Error()}
	}
	if err := validateManifest(manifest, name); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// LoadGroundTruth validates and decodes one ground-truth document.
func (l *Loader) LoadGroundTruth(name string) (GroundTruth, error) {
	var groundTruth GroundTruth
	if l == nil {
		return groundTruth, &Error{Layer: LayerReference, Source: name, Message: "nil loader"}
	}
	if err := validateDatasetPath(name); err != nil {
		return groundTruth, &Error{Layer: LayerReference, Source: name, Message: err.Error()}
	}
	data, err := fs.ReadFile(l.datasetFS, name)
	if err != nil {
		return groundTruth, &Error{Layer: LayerReference, Source: name, Message: fmt.Sprintf("read ground truth: %v", err)}
	}
	if err := validateSchema(l.groundTruthSchema, name, data); err != nil {
		return groundTruth, err
	}
	if err := json.Unmarshal(data, &groundTruth); err != nil {
		return groundTruth, &Error{Layer: LayerSchema, Source: name, Message: err.Error()}
	}
	if err := validateGroundTruth(groundTruth, name); err != nil {
		return groundTruth, err
	}
	return groundTruth, nil
}

// LoadDataset loads all ground truths in manifest order and validates references.
func (l *Loader) LoadDataset(manifestPath string) (Dataset, error) {
	manifest, err := l.LoadManifest(manifestPath)
	if err != nil {
		return Dataset{}, err
	}
	loaded := Dataset{
		Manifest:  manifest,
		TestCases: make([]LoadedTestCase, 0, len(manifest.TestCases)),
	}
	for i, testCase := range manifest.TestCases {
		groundTruth, err := l.LoadGroundTruth(testCase.GroundTruthPath)
		if err != nil {
			return Dataset{}, err
		}
		if err := validateReferences(testCase, groundTruth, testCase.GroundTruthPath, i); err != nil {
			return Dataset{}, err
		}
		if err := l.validateMedia(testCase, manifestPath, i); err != nil {
			return Dataset{}, err
		}
		loaded.TestCases = append(loaded.TestCases, LoadedTestCase{
			TestCase:    testCase,
			GroundTruth: groundTruth,
		})
	}
	return loaded, nil
}

func validateReferences(testCase TestCase, groundTruth GroundTruth, source string, testCaseIndex int) error {
	var errs ErrorList
	if groundTruth.TestCaseID != testCase.TestCaseID {
		errs = append(errs, Error{
			Layer: LayerReference, Source: source, Path: "testCaseId",
			Message: fmt.Sprintf("must exactly match manifest testCases[%d].testCaseId %q", testCaseIndex, testCase.TestCaseID),
		})
	}
	if groundTruth.Content.ContentID != testCase.Content.ContentID {
		errs = append(errs, Error{
			Layer: LayerReference, Source: source, Path: "content.contentId",
			Message: fmt.Sprintf("must exactly match manifest value %q", testCase.Content.ContentID),
		})
	}
	if groundTruth.Content.ContentType != testCase.Content.ContentType {
		errs = append(errs, Error{
			Layer: LayerReference, Source: source, Path: "content.contentType",
			Message: fmt.Sprintf("must exactly match manifest value %q", testCase.Content.ContentType),
		})
	}
	for i, annotation := range groundTruth.Annotations {
		if annotation.Window.EndMs > testCase.Media.DurationMs {
			errs = append(errs, Error{
				Layer: LayerDomain, Source: source, Path: fmt.Sprintf("annotations[%d].window.endMs", i),
				Message: fmt.Sprintf("endMs must be <= media durationMs %d", testCase.Media.DurationMs),
			})
		}
	}
	return errs.Err()
}

func (l *Loader) validateMedia(testCase TestCase, manifestPath string, testCaseIndex int) error {
	if testCase.Media.Kind == MediaKindControlledSource {
		return nil
	}
	data, err := fs.ReadFile(l.datasetFS, testCase.Media.URI)
	path := fmt.Sprintf("testCases[%d].media.uri", testCaseIndex)
	if err != nil {
		return &Error{
			Layer: LayerReference, Source: manifestPath, Path: path,
			Message: fmt.Sprintf("read media %q: %v", testCase.Media.URI, err),
		}
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != testCase.Media.SHA256 {
		return &Error{
			Layer: LayerReference, Source: manifestPath,
			Path:    fmt.Sprintf("testCases[%d].media.sha256", testCaseIndex),
			Message: fmt.Sprintf("digest mismatch for %q: expected %s, got %s", testCase.Media.URI, testCase.Media.SHA256, actual),
		}
	}
	return nil
}
