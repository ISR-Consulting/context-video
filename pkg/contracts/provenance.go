package contracts

// ObservationProvenance records how an observation was produced.
// Provider and model are descriptive labels, not provider SDK payloads.
type ObservationProvenance struct {
	Provider        string  `json:"provider"`
	Model           *string `json:"model,omitempty"`
	PipelineVersion string  `json:"pipelineVersion"`
}

// ContextProvenance records how a context event was fused.
type ContextProvenance struct {
	PipelineVersion string  `json:"pipelineVersion"`
	FusionProvider  *string `json:"fusionProvider,omitempty"`
	FusionModel     *string `json:"fusionModel,omitempty"`
	PromptVersion   *string `json:"promptVersion,omitempty"`
}
