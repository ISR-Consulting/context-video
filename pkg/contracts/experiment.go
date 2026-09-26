package contracts

import "time"

// ExperimentMetrics holds optional experiment measurements.
// A nil field is absent. A pointer to 0 is a measured zero.
type ExperimentMetrics struct {
	LatencyP50Ms         *float64 `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms         *float64 `json:"latencyP95Ms,omitempty"`
	LatencyP99Ms         *float64 `json:"latencyP99Ms,omitempty"`
	CostPerVideoHour     *float64 `json:"costPerVideoHour,omitempty"`
	SchemaCompliance     *float64 `json:"schemaCompliance,omitempty"`
	EvidenceTraceability *float64 `json:"evidenceTraceability,omitempty"`
	ContextStability     *float64 `json:"contextStability,omitempty"`
}

// ExperimentResult is the metrics envelope for one experiment run.
// It does not embed context events or observations.
type ExperimentResult struct {
	ExperimentID  string            `json:"experimentId"`
	StartedAt     time.Time         `json:"startedAt"`
	Configuration map[string]any    `json:"configuration"`
	Metrics       ExperimentMetrics `json:"metrics"`
}
