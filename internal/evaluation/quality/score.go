package quality

import (
	"cmp"
	"slices"

	"github.com/ISR-Consulting/context-video/internal/evaluation/dataset"
	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

// Family is one scored label family.
type Family string

const (
	FamilyEntity Family = "entity"
	FamilyTopic  Family = "topic"
	FamilyObject Family = "object"
	FamilyBrand  Family = "brand"
)

// Label is a comparable ground-truth or predicted label.
type Label struct {
	Family Family
	Type   string // entity type only; empty for topic/object/brand
	Value  string
}

// FamilyCounts holds per-family tallies over annotation windows.
type FamilyCounts struct {
	Required      int `json:"required"`
	RequiredHits  int `json:"requiredHits"`
	ForbiddenHits int `json:"forbiddenHits"`
}

// CaseScore is the lexical score of one test case.
type CaseScore struct {
	TestCaseID         string                  `json:"testCaseId"`
	EventCount         int                     `json:"eventCount"`
	ClipRequiredTotal  int                     `json:"clipRequiredTotal"`
	ClipRequiredHits   int                     `json:"clipRequiredHits"`
	ClipRequiredRecall float64                 `json:"clipRequiredRecall"`
	ClipPredictedTotal int                     `json:"clipPredictedTotal"`
	ClipForbiddenHits  int                     `json:"clipForbiddenHits"`
	ByFamily           map[Family]FamilyCounts `json:"byFamily"`
	MissingRequired    []Label                 `json:"missingRequired,omitempty"`
	HitRequired        []Label                 `json:"hitRequired,omitempty"`
}

// ScoreCase compares events to one ground-truth document under the M09
// lexical overlap policy documented in doc.go.
func ScoreCase(gt dataset.GroundTruth, events []contracts.ContextEventV1) CaseScore {
	out := CaseScore{
		TestCaseID: gt.TestCaseID,
		EventCount: len(events),
		ByFamily: map[Family]FamilyCounts{
			FamilyEntity: {},
			FamilyTopic:  {},
			FamilyObject: {},
			FamilyBrand:  {},
		},
	}

	allReq := map[Label]struct{}{}
	allPred := map[Label]struct{}{}
	allForb := map[Label]struct{}{}
	hitReq := map[Label]struct{}{}

	for _, ann := range gt.Annotations {
		pred := map[Label]struct{}{}
		for _, ev := range events {
			if overlaps(ann.Window, ev.Window) {
				for _, l := range eventLabels(ev) {
					pred[l] = struct{}{}
					allPred[l] = struct{}{}
				}
			}
		}

		req := setLabels(ann.Expected.Required)
		opt := map[Label]struct{}{}
		forb := map[Label]struct{}{}
		if ann.Expected.Optional != nil {
			opt = setLabels(*ann.Expected.Optional)
		}
		if ann.Expected.Forbidden != nil {
			forb = setLabels(*ann.Expected.Forbidden)
		}
		_ = opt // optional is intentionally ignored for scoring

		for l := range req {
			allReq[l] = struct{}{}
			fc := out.ByFamily[l.Family]
			fc.Required++
			if _, ok := pred[l]; ok {
				fc.RequiredHits++
				hitReq[l] = struct{}{}
			}
			out.ByFamily[l.Family] = fc
		}
		for l := range forb {
			allForb[l] = struct{}{}
			if _, ok := pred[l]; ok {
				fc := out.ByFamily[l.Family]
				fc.ForbiddenHits++
				out.ByFamily[l.Family] = fc
			}
		}
	}

	out.ClipRequiredTotal = len(allReq)
	out.ClipPredictedTotal = len(allPred)
	for l := range allReq {
		if _, ok := allPred[l]; ok {
			out.ClipRequiredHits++
			out.HitRequired = append(out.HitRequired, l)
		} else {
			out.MissingRequired = append(out.MissingRequired, l)
		}
	}
	for l := range allForb {
		if _, ok := allPred[l]; ok {
			out.ClipForbiddenHits++
		}
	}
	if out.ClipRequiredTotal > 0 {
		out.ClipRequiredRecall = float64(out.ClipRequiredHits) / float64(out.ClipRequiredTotal)
	}
	slices.SortFunc(out.MissingRequired, compareLabel)
	slices.SortFunc(out.HitRequired, compareLabel)
	return out
}

func compareLabel(a, b Label) int {
	if c := cmp.Compare(string(a.Family), string(b.Family)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Type, b.Type); c != 0 {
		return c
	}
	return cmp.Compare(a.Value, b.Value)
}

func overlaps(a, b contracts.TimeWindow) bool {
	return a.StartMs < b.EndMs && b.StartMs < a.EndMs
}

func setLabels(s dataset.ExpectedSet) map[Label]struct{} {
	out := map[Label]struct{}{}
	for _, e := range s.Entities {
		out[Label{Family: FamilyEntity, Type: e.Type, Value: e.Value}] = struct{}{}
	}
	for _, v := range s.Topics {
		out[Label{Family: FamilyTopic, Value: v}] = struct{}{}
	}
	for _, v := range s.Objects {
		out[Label{Family: FamilyObject, Value: v}] = struct{}{}
	}
	for _, v := range s.Brands {
		out[Label{Family: FamilyBrand, Value: v}] = struct{}{}
	}
	return out
}

func eventLabels(ev contracts.ContextEventV1) []Label {
	var out []Label
	for _, e := range ev.Context.Entities {
		out = append(out, Label{Family: FamilyEntity, Type: e.Type, Value: e.Value})
	}
	for _, t := range ev.Context.Topics {
		out = append(out, Label{Family: FamilyTopic, Value: t.Value})
	}
	for _, o := range ev.Context.Objects {
		out = append(out, Label{Family: FamilyObject, Value: o.Value})
	}
	for _, b := range ev.Context.Brands {
		out = append(out, Label{Family: FamilyBrand, Value: b.Value})
	}
	return out
}
