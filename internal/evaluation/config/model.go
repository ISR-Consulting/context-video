package config

import "time"

// IngestionMode is the ingestion strategy selector declared by an experiment.
type IngestionMode string

// IngestionLiveSimulation selects Live-like replay. M03 accepts the selector
// but does not implement replay.
const IngestionLiveSimulation IngestionMode = "LIVE_SIMULATION"

// ContextEventSelectorV1 is the YAML selector for the frozen ContextEvent v1 contract.
const ContextEventSelectorV1 = "v1"

// file mirrors the YAML layout exactly. Pointers distinguish missing values
// from zero values so required fields can be reported instead of defaulted.
type file struct {
	Experiment *experimentSection `yaml:"experiment"`
	Ingestion  *ingestionSection  `yaml:"ingestion"`
	Window     *windowSection     `yaml:"window"`
	Audio      *audioSection      `yaml:"audio"`
	Vision     *visionSection     `yaml:"vision"`
	Fusion     *fusionSection     `yaml:"fusion"`
	Schema     *schemaSection     `yaml:"schema"`
}

type experimentSection struct {
	ID   *string `yaml:"id"`
	Name *string `yaml:"name"`
}

type ingestionSection struct {
	Mode *string `yaml:"mode"`
}

type windowSection struct {
	Size *string `yaml:"size"`
}

type audioSection struct {
	Enabled  *bool   `yaml:"enabled"`
	Provider *string `yaml:"provider"`
}

type visionSection struct {
	Enabled  *bool   `yaml:"enabled"`
	Provider *string `yaml:"provider"`
	Sampling *string `yaml:"sampling"`
}

type fusionSection struct {
	Provider *string `yaml:"provider"`
}

type schemaSection struct {
	ContextEvent *string `yaml:"context_event"`
}

// Config is a validated experiment configuration.
type Config struct {
	ExperimentID   string
	ExperimentName string
	IngestionMode  IngestionMode
	// WindowSizeRaw is the window.size value exactly as written in YAML.
	WindowSizeRaw  string
	WindowSize     time.Duration
	Audio          AudioConfig
	Vision         VisionConfig
	FusionProvider string
	// ContextEventSelector is the YAML schema.context_event value.
	ContextEventSelector string
	// ContextEventSchemaVersion is the frozen payload schemaVersion the selector maps to.
	ContextEventSchemaVersion string
}

// AudioConfig describes the audio modality. Provider is empty when disabled.
type AudioConfig struct {
	Enabled  bool
	Provider string
}

// VisionConfig describes the visual modality. Provider and Sampling are empty when disabled.
type VisionConfig struct {
	Enabled  bool
	Provider string
	Sampling string
}

// DatasetIdentity pins the golden dataset an experiment run used.
type DatasetIdentity struct {
	DatasetID      string
	DatasetVersion string
	ManifestPath   string
}

// Snapshot returns a JSON-compatible representation of the configuration for
// ExperimentResult.configuration. It preserves the YAML sections and keys and
// adds a "dataset" section identifying the dataset the run used. Disabled
// modalities only carry "enabled", matching the source files.
func (c Config) Snapshot(dataset DatasetIdentity) map[string]any {
	audio := map[string]any{"enabled": c.Audio.Enabled}
	if c.Audio.Enabled {
		audio["provider"] = c.Audio.Provider
	}
	vision := map[string]any{"enabled": c.Vision.Enabled}
	if c.Vision.Enabled {
		vision["provider"] = c.Vision.Provider
		vision["sampling"] = c.Vision.Sampling
	}
	return map[string]any{
		"experiment": map[string]any{
			"id":   c.ExperimentID,
			"name": c.ExperimentName,
		},
		"ingestion": map[string]any{"mode": string(c.IngestionMode)},
		"window":    map[string]any{"size": c.WindowSizeRaw},
		"audio":     audio,
		"vision":    vision,
		"fusion":    map[string]any{"provider": c.FusionProvider},
		"schema":    map[string]any{"context_event": c.ContextEventSelector},
		"dataset": map[string]any{
			"datasetId":      dataset.DatasetID,
			"datasetVersion": dataset.DatasetVersion,
			"manifestPath":   dataset.ManifestPath,
		},
	}
}
