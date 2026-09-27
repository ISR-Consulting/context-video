package dataset

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

func validateManifest(manifest Manifest, source string) error {
	var errs ErrorList
	requireText(manifest.DatasetID, "datasetId", source, &errs)
	requireText(manifest.DatasetVersion, "datasetVersion", source, &errs)
	requireText(manifest.Name, "name", source, &errs)
	requireText(manifest.AnnotationGuidelinesVersion, "annotationGuidelinesVersion", source, &errs)

	seen := make(map[string]int, len(manifest.TestCases))
	for i, testCase := range manifest.TestCases {
		base := fmt.Sprintf("testCases[%d]", i)
		requireText(testCase.TestCaseID, base+".testCaseId", source, &errs)
		if previous, ok := seen[testCase.TestCaseID]; ok {
			errs = append(errs, Error{
				Layer:   LayerDomain,
				Source:  source,
				Path:    base + ".testCaseId",
				Message: fmt.Sprintf("duplicate testCaseId %q; first used at testCases[%d]", testCase.TestCaseID, previous),
			})
		} else {
			seen[testCase.TestCaseID] = i
		}
		if err := validateDatasetPath(testCase.GroundTruthPath); err != nil {
			errs = append(errs, Error{
				Layer:   LayerDomain,
				Source:  source,
				Path:    base + ".groundTruthPath",
				Message: err.Error(),
			})
		}
		requireText(testCase.Media.URI, base+".media.uri", source, &errs)
		if testCase.Media.Kind == MediaKindLocal || testCase.Media.Kind == MediaKindFixture {
			if err := validateDatasetPath(testCase.Media.URI); err != nil {
				errs = append(errs, Error{
					Layer:   LayerDomain,
					Source:  source,
					Path:    base + ".media.uri",
					Message: err.Error(),
				})
			}
		}
	}
	return errs.Err()
}

func validateGroundTruth(groundTruth GroundTruth, source string) error {
	var errs ErrorList
	requireText(groundTruth.TestCaseID, "testCaseId", source, &errs)
	seen := make(map[string]int, len(groundTruth.Annotations))
	for i, annotation := range groundTruth.Annotations {
		base := fmt.Sprintf("annotations[%d]", i)
		requireText(annotation.AnnotationID, base+".annotationId", source, &errs)
		if previous, ok := seen[annotation.AnnotationID]; ok {
			errs = append(errs, Error{
				Layer:   LayerDomain,
				Source:  source,
				Path:    base + ".annotationId",
				Message: fmt.Sprintf("duplicate annotationId %q; first used at annotations[%d]", annotation.AnnotationID, previous),
			})
		} else {
			seen[annotation.AnnotationID] = i
		}
		if annotation.Window.StartMs < 0 {
			errs = append(errs, Error{
				Layer: LayerDomain, Source: source, Path: base + ".window.startMs",
				Message: "startMs must be >= 0",
			})
		}
		if annotation.Window.EndMs <= annotation.Window.StartMs {
			errs = append(errs, Error{
				Layer: LayerDomain, Source: source, Path: base + ".window.endMs",
				Message: "endMs must be > startMs",
			})
		}
		validateExpected(annotation, base, source, &errs)
	}
	return errs.Err()
}

func validateExpected(annotation GroundTruthAnnotation, base, source string, errs *ErrorList) {
	groups := []struct {
		name string
		set  *ExpectedSet
	}{
		{name: "required", set: &annotation.Expected.Required},
		{name: "optional", set: annotation.Expected.Optional},
		{name: "forbidden", set: annotation.Expected.Forbidden},
	}
	entityGroups := make(map[string]string)
	topicGroups := make(map[string]string)
	objectGroups := make(map[string]string)
	brandGroups := make(map[string]string)
	positiveCount := 0
	optionalCount := 0

	for _, group := range groups {
		if group.set == nil {
			continue
		}
		groupBase := base + ".expected." + group.name
		if group.name != "forbidden" {
			positiveCount += expectedSetSize(*group.set)
		}
		if group.name == "optional" {
			optionalCount = expectedSetSize(*group.set)
		}
		validateEntities(group.set.Entities, group.name, groupBase+".entities", source, entityGroups, errs)
		validateStrings(group.set.Topics, group.name, groupBase+".topics", source, topicGroups, errs)
		validateStrings(group.set.Objects, group.name, groupBase+".objects", source, objectGroups, errs)
		validateStrings(group.set.Brands, group.name, groupBase+".brands", source, brandGroups, errs)
	}

	if positiveCount == 0 {
		*errs = append(*errs, Error{
			Layer: LayerDomain, Source: source, Path: base + ".expected",
			Message: "at least one required or optional expectation is required",
		})
	}
	if annotation.AmbiguityNote != nil && strings.TrimSpace(*annotation.AmbiguityNote) == "" {
		*errs = append(*errs, Error{
			Layer: LayerDomain, Source: source, Path: base + ".ambiguityNote",
			Message: "ambiguityNote must not be blank",
		})
	} else if optionalCount > 0 && annotation.AmbiguityNote == nil {
		*errs = append(*errs, Error{
			Layer: LayerDomain, Source: source, Path: base + ".ambiguityNote",
			Message: "ambiguityNote is required when optional expectations are present",
		})
	}
}

func validateEntities(values []ExpectedEntity, group, base, source string, seen map[string]string, errs *ErrorList) {
	for i, entity := range values {
		itemPath := fmt.Sprintf("%s[%d]", base, i)
		requireText(entity.Type, itemPath+".type", source, errs)
		requireText(entity.Value, itemPath+".value", source, errs)
		key := entity.Type + "\x00" + entity.Value
		checkDuplicate(key, "entity", group, itemPath, source, seen, errs)
	}
}

func validateStrings(values []string, group, base, source string, seen map[string]string, errs *ErrorList) {
	for i, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", base, i)
		requireText(value, itemPath, source, errs)
		checkDuplicate(value, "label", group, itemPath, source, seen, errs)
	}
}

func checkDuplicate(key, kind, group, itemPath, source string, seen map[string]string, errs *ErrorList) {
	if previousGroup, ok := seen[key]; ok {
		message := fmt.Sprintf("duplicate %s in %s expectations", kind, group)
		if previousGroup != group {
			message = fmt.Sprintf("%s appears in both %s and %s expectations", kind, previousGroup, group)
		}
		*errs = append(*errs, Error{
			Layer: LayerDomain, Source: source, Path: itemPath, Message: message,
		})
		return
	}
	seen[key] = group
}

func expectedSetSize(set ExpectedSet) int {
	return len(set.Entities) + len(set.Topics) + len(set.Objects) + len(set.Brands)
}

func requireText(value, fieldPath, source string, errs *ErrorList) {
	if strings.TrimSpace(value) == "" {
		*errs = append(*errs, Error{
			Layer: LayerDomain, Source: source, Path: fieldPath, Message: "must not be blank",
		})
	}
}

func validateDatasetPath(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("path must not be empty")
	case strings.Contains(name, `\`):
		return fmt.Errorf("path must use / separators")
	case name == ".":
		return fmt.Errorf("path must identify a file")
	case path.Clean(name) != name:
		return fmt.Errorf("path must be clean")
	case !fs.ValidPath(name):
		return fmt.Errorf("path must be relative and cannot escape the dataset filesystem")
	default:
		return nil
	}
}
