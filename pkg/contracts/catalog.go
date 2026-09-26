package contracts

import "fmt"

// Catalog is the set of observations available when checking evidence references.
type Catalog struct {
	audio  map[string]AudioObservation
	visual map[string]VisualObservation
}

// NewCatalog indexes observations by observationId.
// Duplicate identifiers and observations that fail Validate are rejected.
func NewCatalog(audio []AudioObservation, visual []VisualObservation) (Catalog, error) {
	catalog := Catalog{
		audio:  make(map[string]AudioObservation, len(audio)),
		visual: make(map[string]VisualObservation, len(visual)),
	}
	var errs ErrorList
	for i, obs := range audio {
		path := fmt.Sprintf("audio[%d]", i)
		if _, exists := catalog.audio[obs.ObservationID]; exists {
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path + ".observationId",
				Message: "duplicate observationId " + obs.ObservationID,
			})
			continue
		}
		if err := Validate(obs); err != nil {
			errs = append(errs, prefixErrors(path, err)...)
		}
		catalog.audio[obs.ObservationID] = obs
	}
	for i, obs := range visual {
		path := fmt.Sprintf("visual[%d]", i)
		_, duplicateVisual := catalog.visual[obs.ObservationID]
		_, duplicateAudio := catalog.audio[obs.ObservationID]
		if duplicateVisual || duplicateAudio {
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path + ".observationId",
				Message: "duplicate observationId " + obs.ObservationID,
			})
			continue
		}
		if err := Validate(obs); err != nil {
			errs = append(errs, prefixErrors(path, err)...)
		}
		catalog.visual[obs.ObservationID] = obs
	}
	if err := errs.Err(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// ValidateEvidence checks that each evidence item cites an observation of the
// matching kind whose content identity equals the event content.
// An event with empty evidence passes. A cited id that is missing fails.
func (e ContextEventV1) ValidateEvidence(catalog Catalog) error {
	var errs ErrorList
	for i, item := range e.Evidence.Audio {
		path := fmt.Sprintf("evidence.audio[%d].observationId", i)
		obs, ok := catalog.audio[item.ObservationID]
		if !ok {
			if _, visual := catalog.visual[item.ObservationID]; visual {
				errs = append(errs, Error{
					Layer:   "domain",
					Path:    path,
					Message: "observationId refers to a visual observation",
				})
				continue
			}
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "observationId not found",
			})
			continue
		}
		if obs.Content != e.Content {
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "content identity does not match the context event",
			})
		}
	}
	for i, item := range e.Evidence.Visual {
		path := fmt.Sprintf("evidence.visual[%d].observationId", i)
		obs, ok := catalog.visual[item.ObservationID]
		if !ok {
			if _, audio := catalog.audio[item.ObservationID]; audio {
				errs = append(errs, Error{
					Layer:   "domain",
					Path:    path,
					Message: "observationId refers to an audio observation",
				})
				continue
			}
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "observationId not found",
			})
			continue
		}
		if obs.Content != e.Content {
			errs = append(errs, Error{
				Layer:   "domain",
				Path:    path,
				Message: "content identity does not match the context event",
			})
		}
	}
	return errs.Err()
}
