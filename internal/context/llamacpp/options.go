package llamacpp

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// Option keys accepted by NewFromOptions.
const (
	OptionModel     = "model"
	OptionBinary    = "binary"
	OptionThreads   = "threads"
	OptionGPULayers = "gpu-layers"
	OptionMaxTokens = "max-tokens"
	OptionCtxSize   = "ctx-size"
)

const (
	defaultBinary    = "llama-completion"
	defaultMaxTokens = 1024
)

// Config configures an Adapter. Binary is a command name resolved through
// PATH or an explicit path.
type Config struct {
	// Model is the GGUF instruction-tuned text model file.
	Model  string
	Binary string
	// Threads is passed as -t when > 0; 0 keeps the llama.cpp default.
	Threads int
	// GPULayers is passed as -ngl when non-nil; nil keeps the llama.cpp default.
	GPULayers *int
	// MaxTokens bounds the generated answer (-n); 0 means 1024.
	MaxTokens int
	// CtxSize is passed as -c when > 0; 0 keeps the model default.
	CtxSize int
}

func configFromOptions(opts contextcore.Options) (Config, error) {
	known := []string{OptionBinary, OptionCtxSize, OptionGPULayers, OptionMaxTokens, OptionModel, OptionThreads}
	var errs []error
	for key := range opts {
		if !slices.Contains(known, key) {
			errs = append(errs, fmt.Errorf("unknown option %q; supported: %s", key, strings.Join(known, ", ")))
		}
	}
	cfg := Config{Model: opts[OptionModel], Binary: opts[OptionBinary]}
	intOption := func(key string, min int, dst *int) bool {
		raw, ok := opts[key]
		if !ok {
			return false
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < min {
			kind := "a positive integer"
			if min == 0 {
				kind = "a non-negative integer"
			}
			errs = append(errs, fmt.Errorf("option %q must be %s, got %q", key, kind, raw))
			return false
		}
		*dst = n
		return true
	}
	intOption(OptionThreads, 1, &cfg.Threads)
	intOption(OptionMaxTokens, 1, &cfg.MaxTokens)
	intOption(OptionCtxSize, 1, &cfg.CtxSize)
	var layers int
	if intOption(OptionGPULayers, 0, &layers) {
		cfg.GPULayers = &layers
	}
	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func (c Config) withDefaults() (Config, error) {
	var errs []error
	if strings.TrimSpace(c.Model) == "" {
		errs = append(errs, fmt.Errorf("option %q (path to a GGUF instruction-tuned text model) is required", OptionModel))
	}
	if c.Threads < 0 || c.MaxTokens < 0 || c.CtxSize < 0 || (c.GPULayers != nil && *c.GPULayers < 0) {
		errs = append(errs, errors.New("threads, gpu-layers, max-tokens and ctx-size must not be negative"))
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	if c.Binary == "" {
		c.Binary = defaultBinary
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultMaxTokens
	}
	return c, nil
}
