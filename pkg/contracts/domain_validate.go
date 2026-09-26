package contracts

import (
	"errors"
	"fmt"
	"strings"
)

// commercialKeys are the JSON object keys forbidden by the contract exclusions.
// They are rejected only inside open objects, where JSON Schema allows extra properties.
var commercialKeys = map[string]struct{}{
	"productId":         {},
	"sku":               {},
	"SKU":               {},
	"retailer":          {},
	"price":             {},
	"inventory":         {},
	"offer":             {},
	"commercialRanking": {},
	"checkout":          {},
}

// Validate checks semantic invariants that JSON Schema does not express.
// It does not resolve observation identifiers. Use ContextEventV1.ValidateEvidence for that.
func Validate(v any) error {
	var errs ErrorList
	if !collectDomain(v, &errs) {
		return &Error{Layer: "domain", Message: fmt.Sprintf("unsupported type %T", v)}
	}
	return errs.Err()
}

func collectDomain(v any, errs *ErrorList) bool {
	switch x := v.(type) {
	case TimeWindow:
		collectInterval(x.StartMs, x.EndMs, "", errs)
	case *TimeWindow:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil TimeWindow"})
			return true
		}
		return collectDomain(*x, errs)
	case MediaSegment:
		collectInterval(x.Window.StartMs, x.Window.EndMs, "window", errs)
		collectCommercialKeys("metadata", x.Metadata, errs)
	case *MediaSegment:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil MediaSegment"})
			return true
		}
		return collectDomain(*x, errs)
	case AudioObservation:
		collectInterval(x.Window.StartMs, x.Window.EndMs, "window", errs)
	case *AudioObservation:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil AudioObservation"})
			return true
		}
		return collectDomain(*x, errs)
	case VisualObservation:
		collectVisualObservation(x, errs)
	case *VisualObservation:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil VisualObservation"})
			return true
		}
		return collectDomain(*x, errs)
	case ContextEventV1:
		collectContextEvent(x, errs)
	case *ContextEventV1:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil ContextEventV1"})
			return true
		}
		return collectDomain(*x, errs)
	case ExperimentResult:
		collectCommercialKeys("configuration", x.Configuration, errs)
	case *ExperimentResult:
		if x == nil {
			*errs = append(*errs, Error{Layer: "domain", Message: "nil ExperimentResult"})
			return true
		}
		return collectDomain(*x, errs)
	default:
		return false
	}
	return true
}

func collectContextEvent(event ContextEventV1, errs *ErrorList) {
	windowOK := intervalOK(event.Window.StartMs, event.Window.EndMs, "window", errs)
	if len(event.Evidence.Audio) == 0 && len(event.Evidence.Visual) == 0 {
		*errs = append(*errs, Error{
			Layer:   "domain",
			Path:    "evidence",
			Message: "at least one audio or visual evidence item is required",
		})
	}
	for i, item := range event.Evidence.Audio {
		base := fmt.Sprintf("evidence.audio[%d]", i)
		itemOK := intervalOK(item.StartMs, item.EndMs, base, errs)
		if windowOK && itemOK && !overlapsClosed(item.StartMs, item.EndMs, event.Window.StartMs, event.Window.EndMs) {
			*errs = append(*errs, Error{
				Layer:   "domain",
				Path:    base,
				Message: "audio evidence interval must overlap the context event window",
			})
		}
	}
	for i, item := range event.Evidence.Visual {
		base := fmt.Sprintf("evidence.visual[%d].timestampMs", i)
		if item.TimestampMs < 0 {
			*errs = append(*errs, Error{
				Layer:   "domain",
				Path:    base,
				Message: "timestampMs must be >= 0",
			})
		}
		if windowOK && item.TimestampMs >= 0 && !containsClosed(event.Window.StartMs, event.Window.EndMs, item.TimestampMs) {
			*errs = append(*errs, Error{
				Layer:   "domain",
				Path:    base,
				Message: "visual evidence timestamp must belong to the context event window",
			})
		}
	}
}

func collectVisualObservation(observation VisualObservation, errs *ErrorList) {
	windowOK := intervalOK(observation.Window.StartMs, observation.Window.EndMs, "window", errs)
	for i, frame := range observation.Frames {
		path := fmt.Sprintf("frames[%d].timestampMs", i)
		if frame.TimestampMs < 0 {
			*errs = append(*errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "timestampMs must be >= 0",
			})
		}
		if windowOK && frame.TimestampMs >= 0 &&
			!containsClosed(observation.Window.StartMs, observation.Window.EndMs, frame.TimestampMs) {
			*errs = append(*errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "frame timestamp must belong to the observation window",
			})
		}
	}
}

func collectInterval(startMs, endMs int64, path string, errs *ErrorList) {
	intervalOK(startMs, endMs, path, errs)
}

// intervalOK reports whether the interval satisfies startMs >= 0 and endMs > startMs.
// Failures are appended to errs.
func intervalOK(startMs, endMs int64, path string, errs *ErrorList) bool {
	ok := true
	if startMs < 0 {
		ok = false
		*errs = append(*errs, Error{
			Layer:   "domain",
			Path:    joinPath(path, "startMs"),
			Message: "startMs must be >= 0",
		})
	}
	if endMs <= startMs {
		ok = false
		*errs = append(*errs, Error{
			Layer:   "domain",
			Path:    joinPath(path, "endMs"),
			Message: "endMs must be > startMs",
		})
	}
	return ok
}

// overlapsClosed reports whether two closed millisecond intervals share a point.
// Sharing an endpoint counts as overlap.
func overlapsClosed(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart <= bEnd && bStart <= aEnd
}

func containsClosed(startMs, endMs, timestampMs int64) bool {
	return startMs <= timestampMs && timestampMs <= endMs
}

func collectCommercialKeys(path string, value any, errs *ErrorList) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			childPath := joinPath(path, key)
			if _, forbidden := commercialKeys[key]; forbidden {
				*errs = append(*errs, Error{
					Layer:   "domain",
					Path:    childPath,
					Message: "commercial field is not allowed",
				})
			}
			collectCommercialKeys(childPath, child, errs)
		}
	case []any:
		for i, child := range node {
			collectCommercialKeys(fmt.Sprintf("%s[%d]", path, i), child, errs)
		}
	}
}

func joinPath(path, field string) string {
	if path == "" {
		return field
	}
	if field == "" {
		return path
	}
	if strings.HasPrefix(field, "[") {
		return path + field
	}
	return path + "." + field
}

func prefixErrors(prefix string, err error) ErrorList {
	if err == nil {
		return nil
	}
	var list ErrorList
	if errors.As(err, &list) {
		out := make(ErrorList, len(list))
		for i, item := range list {
			item.Path = joinPath(prefix, item.Path)
			out[i] = item
		}
		return out
	}
	var one *Error
	if errors.As(err, &one) {
		cp := *one
		cp.Path = joinPath(prefix, cp.Path)
		return ErrorList{cp}
	}
	return ErrorList{{Layer: "domain", Path: prefix, Message: err.Error()}}
}
