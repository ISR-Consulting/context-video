package llamamtmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// maxExcerpt bounds the model output quoted in parse errors.
const maxExcerpt = 512

// answer is the document the prompt and Grammar ask for.
type answer struct {
	Description  *string `json:"description"`
	Observations *[]struct {
		Type       string   `json:"type"`
		Value      string   `json:"value"`
		Confidence *float64 `json:"confidence"`
	} `json:"observations"`
}

// parseAnswer strictly decodes one model answer for the frame at timestampMs.
// A single surrounding Markdown code fence is tolerated; anything else that is
// not exactly one conforming JSON object is an error.
func parseAnswer(stdout []byte, timestampMs int64) (vision.Frame, error) {
	body := unfence(bytes.TrimSpace(stdout))
	if len(body) == 0 {
		return vision.Frame{}, errors.New("empty model answer")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var a answer
	if err := dec.Decode(&a); err != nil {
		return vision.Frame{}, fmt.Errorf("decode model answer: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return vision.Frame{}, errors.New("decode model answer: unexpected data after the JSON object")
	}
	if a.Observations == nil {
		return vision.Frame{}, errors.New(`model answer has no "observations" array`)
	}

	types := vision.DetectionTypes()
	var errs []error
	frame := vision.Frame{TimestampMs: timestampMs, Detections: make([]vision.Detection, 0, len(*a.Observations))}
	if a.Description != nil {
		frame.Description = strings.TrimSpace(*a.Description)
	}
	if len(*a.Observations) > MaxDetections {
		errs = append(errs, fmt.Errorf("%d observations, more than the %d allowed", len(*a.Observations), MaxDetections))
	}
	for i, o := range *a.Observations {
		t := contracts.VisualDetectionType(o.Type)
		value := strings.TrimSpace(o.Value)
		switch {
		case !slices.Contains(types, t):
			errs = append(errs, fmt.Errorf("observation %d: type %q is not one of %v", i, o.Type, types))
		case value == "":
			errs = append(errs, fmt.Errorf("observation %d: blank value", i))
		case slices.Contains(snakeTypes, t) && !isSnake(value):
			errs = append(errs, fmt.Errorf("observation %d: %s value %q is not lowercase snake_case", i, t, value))
		case o.Confidence == nil:
			errs = append(errs, fmt.Errorf("observation %d: missing confidence", i))
		case math.IsNaN(*o.Confidence) || *o.Confidence < 0 || *o.Confidence > 1:
			errs = append(errs, fmt.Errorf("observation %d: confidence %v outside [0, 1]", i, *o.Confidence))
		default:
			confidence := *o.Confidence
			frame.Detections = append(frame.Detections, vision.Detection{Type: t, Value: value, Confidence: &confidence})
		}
	}
	if len(errs) > 0 {
		return vision.Frame{}, errors.Join(errs...)
	}
	return frame, nil
}

func unfence(b []byte) []byte {
	if !bytes.HasPrefix(b, []byte("```")) || !bytes.HasSuffix(b, []byte("```")) || len(b) < 6 {
		return b
	}
	inner := b[3 : len(b)-3]
	if nl := bytes.IndexByte(inner, '\n'); nl >= 0 && !bytes.ContainsAny(inner[:nl], "{[") {
		inner = inner[nl+1:]
	}
	return bytes.TrimSpace(inner)
}

func excerpt(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > maxExcerpt {
		s = s[:maxExcerpt] + "…"
	}
	return s
}
