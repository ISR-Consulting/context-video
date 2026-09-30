package llamacpp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// maxExcerpt bounds the model output quoted in parse errors.
const maxExcerpt = 512

// endOfText is the marker llama-completion prints after the answer when
// generation stops at the end-of-generation token. It is CLI output, not model
// content.
var endOfText = []byte("[end of text]")

type answer struct {
	Events *[]answerEvent `json:"events"`
}

type answerEvent struct {
	Entities   *[]answerEntity `json:"entities"`
	Topics     *[]answerValue  `json:"topics"`
	Objects    *[]answerValue  `json:"objects"`
	Brands     *[]answerValue  `json:"brands"`
	Confidence *float64        `json:"confidence"`
	Evidence   *[]answerRef    `json:"evidence"`
}

type answerEntity struct {
	Type       string   `json:"type"`
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence"`
}

type answerValue struct {
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence"`
}

type answerRef struct {
	ObservationID string `json:"observationId"`
	TimestampMs   *int64 `json:"timestampMs"`
}

// parseAnswer strictly decodes one model answer into candidates. The trailing
// end-of-text marker and a single surrounding Markdown code fence are
// tolerated; anything else that is not exactly one conforming JSON object is
// an error. Values are not trimmed, clamped or defaulted; grounding against
// the evidence is left to the core.
func parseAnswer(stdout []byte, meta contextcore.ReasoningMetadata) ([]contextcore.Candidate, error) {
	body := bytes.TrimSpace(stdout)
	body = bytes.TrimSpace(bytes.TrimSuffix(body, endOfText))
	body = unfence(body)
	if len(body) == 0 {
		return nil, errors.New("empty model answer")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var a answer
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("decode model answer: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode model answer: unexpected data after the JSON object")
	}
	if a.Events == nil {
		return nil, errors.New(`model answer has no "events" array`)
	}

	var errs []error
	candidates := make([]contextcore.Candidate, 0, len(*a.Events))
	for i, ev := range *a.Events {
		where := fmt.Sprintf("events[%d]", i)
		missing := func(field string) { errs = append(errs, fmt.Errorf("%s: missing %q", where, field)) }
		switch {
		case ev.Entities == nil:
			missing("entities")
			continue
		case ev.Topics == nil:
			missing("topics")
			continue
		case ev.Objects == nil:
			missing("objects")
			continue
		case ev.Brands == nil:
			missing("brands")
			continue
		case ev.Evidence == nil:
			missing("evidence")
			continue
		}
		c := contextcore.Candidate{
			Confidence: ev.Confidence,
			Reasoning:  meta,
			Entities:   make([]contextcore.Entity, 0, len(*ev.Entities)),
			Topics:     values(*ev.Topics),
			Objects:    values(*ev.Objects),
			Brands:     values(*ev.Brands),
			Evidence:   make([]contextcore.EvidenceRef, 0, len(*ev.Evidence)),
		}
		errs = checkConfidence(errs, where+".confidence", ev.Confidence)
		for j, e := range *ev.Entities {
			errs = checkConfidence(errs, fmt.Sprintf("%s.entities[%d].confidence", where, j), e.Confidence)
			c.Entities = append(c.Entities, contextcore.Entity{Type: e.Type, Value: e.Value, Confidence: e.Confidence})
		}
		for _, list := range []struct {
			name   string
			values []answerValue
		}{{"topics", *ev.Topics}, {"objects", *ev.Objects}, {"brands", *ev.Brands}} {
			for j, v := range list.values {
				errs = checkConfidence(errs, fmt.Sprintf("%s.%s[%d].confidence", where, list.name, j), v.Confidence)
			}
		}
		for _, ref := range *ev.Evidence {
			c.Evidence = append(c.Evidence, contextcore.EvidenceRef{ObservationID: ref.ObservationID, TimestampMs: ref.TimestampMs})
		}
		candidates = append(candidates, c)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return candidates, nil
}

func values(in []answerValue) []contextcore.Value {
	out := make([]contextcore.Value, 0, len(in))
	for _, v := range in {
		out = append(out, contextcore.Value{Value: v.Value, Confidence: v.Confidence})
	}
	return out
}

func checkConfidence(errs []error, where string, c *float64) []error {
	switch {
	case c == nil:
		return append(errs, fmt.Errorf("%s: missing", where))
	case math.IsNaN(*c) || *c < 0 || *c > 1:
		return append(errs, fmt.Errorf("%s: %v outside [0, 1]", where, *c))
	}
	return errs
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
	s := string(bytes.TrimSpace(b))
	if len(s) > maxExcerpt {
		s = s[:maxExcerpt] + "…"
	}
	return s
}
