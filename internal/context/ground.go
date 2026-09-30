package context

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Ground checks a candidate against the group it was reasoned from. It is the
// hallucination containment boundary: every cited observation must be in the
// group, audio references carry no timestamp, visual references cite a frame
// the group actually holds, and nothing may be cited twice. The candidate must
// assert at least one non-blank, non-duplicate item, and every item and the
// event must carry a stated confidence in [0, 1]. Nothing is repaired,
// trimmed, clamped or defaulted.
func Ground(group EvidenceGroup, c Candidate) error {
	audio := make(map[string]struct{})
	visual := make(map[string]map[int64]struct{})
	for _, e := range group.Evidence {
		switch e.Kind {
		case EvidenceAudio:
			audio[e.ObservationID] = struct{}{}
		case EvidenceVisual:
			if e.TimestampMs == nil {
				continue
			}
			if visual[e.ObservationID] == nil {
				visual[e.ObservationID] = make(map[int64]struct{})
			}
			visual[e.ObservationID][*e.TimestampMs] = struct{}{}
		}
	}

	var errs []error
	if len(c.Evidence) == 0 {
		errs = append(errs, errors.New("candidate cites no evidence"))
	}
	type key struct {
		id string
		ts int64
		at bool
	}
	cited := make(map[key]struct{}, len(c.Evidence))
	for i, ref := range c.Evidence {
		k := key{id: ref.ObservationID}
		if ref.TimestampMs != nil {
			k.ts, k.at = *ref.TimestampMs, true
		}
		if _, dup := cited[k]; dup {
			errs = append(errs, fmt.Errorf("evidence[%d]: duplicate reference to %s", i, describeRef(ref)))
			continue
		}
		cited[k] = struct{}{}
		_, isAudio := audio[ref.ObservationID]
		frames, isVisual := visual[ref.ObservationID]
		switch {
		case isAudio && ref.TimestampMs != nil:
			errs = append(errs, fmt.Errorf("evidence[%d]: audio observation %s cited with a timestamp", i, ref.ObservationID))
		case isAudio:
		case isVisual && ref.TimestampMs == nil:
			errs = append(errs, fmt.Errorf("evidence[%d]: visual observation %s cited without a frame timestamp", i, ref.ObservationID))
		case isVisual:
			if _, ok := frames[*ref.TimestampMs]; !ok {
				errs = append(errs, fmt.Errorf("evidence[%d]: unknown frame %dms of visual observation %s", i, *ref.TimestampMs, ref.ObservationID))
			}
		default:
			errs = append(errs, fmt.Errorf("evidence[%d]: unknown observationId %q", i, ref.ObservationID))
		}
	}

	if len(c.Entities)+len(c.Topics)+len(c.Objects)+len(c.Brands) == 0 {
		errs = append(errs, errors.New("candidate asserts no entity, topic, object or brand"))
	}
	entities := make(map[[2]string]struct{}, len(c.Entities))
	for i, e := range c.Entities {
		where := fmt.Sprintf("entities[%d]", i)
		if strings.TrimSpace(e.Type) == "" {
			errs = append(errs, fmt.Errorf("%s: blank type", where))
		}
		if strings.TrimSpace(e.Value) == "" {
			errs = append(errs, fmt.Errorf("%s: blank value", where))
		}
		k := [2]string{e.Type, e.Value}
		if _, dup := entities[k]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate entity %s %q", where, e.Type, e.Value))
		}
		entities[k] = struct{}{}
		errs = appendConfidence(errs, where, e.Confidence)
	}
	for _, list := range []struct {
		name   string
		values []Value
	}{{"topics", c.Topics}, {"objects", c.Objects}, {"brands", c.Brands}} {
		seen := make(map[string]struct{}, len(list.values))
		for i, v := range list.values {
			where := fmt.Sprintf("%s[%d]", list.name, i)
			if strings.TrimSpace(v.Value) == "" {
				errs = append(errs, fmt.Errorf("%s: blank value", where))
			}
			if _, dup := seen[v.Value]; dup {
				errs = append(errs, fmt.Errorf("%s: duplicate value %q", where, v.Value))
			}
			seen[v.Value] = struct{}{}
			errs = appendConfidence(errs, where, v.Confidence)
		}
	}
	errs = appendConfidence(errs, "confidence", c.Confidence)

	if strings.TrimSpace(c.Reasoning.Provider) == "" {
		errs = append(errs, errors.New("reasoning provider must not be blank"))
	}
	if strings.ContainsAny(c.Reasoning.Model, `/\`) {
		errs = append(errs, fmt.Errorf("reasoning model %q must be a label, not a path", c.Reasoning.Model))
	}
	return errors.Join(errs...)
}

func appendConfidence(errs []error, where string, c *float64) []error {
	switch {
	case c == nil:
		return append(errs, fmt.Errorf("%s: missing confidence", where))
	case math.IsNaN(*c) || math.IsInf(*c, 0) || *c < 0 || *c > 1:
		return append(errs, fmt.Errorf("%s: confidence %v outside [0, 1]", where, *c))
	}
	return errs
}

func describeRef(ref EvidenceRef) string {
	if ref.TimestampMs == nil {
		return ref.ObservationID
	}
	return fmt.Sprintf("%s@%dms", ref.ObservationID, *ref.TimestampMs)
}
