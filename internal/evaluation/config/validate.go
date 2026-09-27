package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/ISR-Consulting/context-video/pkg/contracts"
)

var ingestionModes = map[IngestionMode]struct{}{
	IngestionLiveSimulation: {},
}

// contextEventSelectors maps YAML schema selectors to frozen payload versions.
var contextEventSelectors = map[string]string{
	ContextEventSelectorV1: contracts.ContextEventSchemaVersion,
}

func validate(source string, raw file) (Config, error) {
	var issues []Issue
	add := func(path, message string) {
		issues = append(issues, Issue{Path: path, Message: message})
	}
	requireText := func(value *string, path string) string {
		if value == nil {
			add(path, "is required")
			return ""
		}
		if strings.TrimSpace(*value) == "" {
			add(path, "must not be blank")
		}
		return *value
	}

	var cfg Config

	if raw.Experiment == nil {
		add("experiment", "section is required")
	} else {
		cfg.ExperimentID = requireText(raw.Experiment.ID, "experiment.id")
		cfg.ExperimentName = requireText(raw.Experiment.Name, "experiment.name")
	}

	if raw.Ingestion == nil {
		add("ingestion", "section is required")
	} else if mode := requireText(raw.Ingestion.Mode, "ingestion.mode"); mode != "" {
		cfg.IngestionMode = IngestionMode(mode)
		if _, ok := ingestionModes[cfg.IngestionMode]; !ok {
			add("ingestion.mode", fmt.Sprintf("unsupported value %q", mode))
		}
	}

	if raw.Window == nil {
		add("window", "section is required")
	} else if size := requireText(raw.Window.Size, "window.size"); size != "" {
		cfg.WindowSizeRaw = size
		if d, err := time.ParseDuration(size); err != nil {
			add("window.size", fmt.Sprintf("invalid duration %q: %v", size, err))
		} else if d <= 0 {
			add("window.size", "must be positive")
		} else if d%time.Millisecond != 0 {
			add("window.size", "must be a whole number of milliseconds")
		} else {
			cfg.WindowSize = d
		}
	}

	if raw.Audio == nil {
		add("audio", "section is required")
	} else if raw.Audio.Enabled == nil {
		add("audio.enabled", "is required")
	} else {
		cfg.Audio.Enabled = *raw.Audio.Enabled
		if cfg.Audio.Enabled {
			cfg.Audio.Provider = requireText(raw.Audio.Provider, "audio.provider")
		} else if raw.Audio.Provider != nil {
			add("audio.provider", "must be omitted when audio is disabled")
		}
	}

	if raw.Vision == nil {
		add("vision", "section is required")
	} else if raw.Vision.Enabled == nil {
		add("vision.enabled", "is required")
	} else {
		cfg.Vision.Enabled = *raw.Vision.Enabled
		if cfg.Vision.Enabled {
			cfg.Vision.Provider = requireText(raw.Vision.Provider, "vision.provider")
			cfg.Vision.Sampling = requireText(raw.Vision.Sampling, "vision.sampling")
		} else {
			if raw.Vision.Provider != nil {
				add("vision.provider", "must be omitted when vision is disabled")
			}
			if raw.Vision.Sampling != nil {
				add("vision.sampling", "must be omitted when vision is disabled")
			}
		}
	}

	if raw.Audio != nil && raw.Audio.Enabled != nil && raw.Vision != nil && raw.Vision.Enabled != nil &&
		!cfg.Audio.Enabled && !cfg.Vision.Enabled {
		add("", "at least one of audio or vision must be enabled")
	}

	if raw.Fusion == nil {
		add("fusion", "section is required")
	} else {
		cfg.FusionProvider = requireText(raw.Fusion.Provider, "fusion.provider")
	}

	if raw.Schema == nil {
		add("schema", "section is required")
	} else {
		cfg.ContextEventSelector = requireText(raw.Schema.ContextEvent, "schema.context_event")
	}

	if len(issues) > 0 {
		return Config{}, &Error{Stage: StageDomain, Source: source, Issues: issues}
	}

	version, ok := contextEventSelectors[cfg.ContextEventSelector]
	if !ok {
		return Config{}, &Error{Stage: StageSchemaSelector, Source: source, Issues: []Issue{{
			Path:    "schema.context_event",
			Message: fmt.Sprintf("unsupported selector %q; supported: %q", cfg.ContextEventSelector, ContextEventSelectorV1),
		}}}
	}
	cfg.ContextEventSchemaVersion = version
	return cfg, nil
}
