package llamamtmd

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/vision"
)

// Option keys accepted by NewFromOptions.
const (
	OptionModel     = "model"
	OptionMMProj    = "mmproj"
	OptionBinary    = "binary"
	OptionFFmpeg    = "ffmpeg"
	OptionThreads   = "threads"
	OptionGPULayers = "gpu-layers"
	OptionMaxTokens = "max-tokens"
	OptionMaxEdge   = "max-edge"
	OptionCtxSize   = "ctx-size"
)

const (
	defaultBinary    = "llama-mtmd-cli"
	defaultFFmpeg    = "ffmpeg"
	defaultMaxTokens = 1024
	defaultMaxEdge   = 768
)

// Config configures an Adapter. Binary and FFmpeg are command names resolved
// through PATH or explicit paths.
type Config struct {
	// Model is the GGUF language model file.
	Model string
	// MMProj is the matching GGUF multimodal projector file.
	MMProj string
	Binary string
	FFmpeg string
	// Threads is passed as -t when > 0; 0 keeps the llama.cpp default.
	Threads int
	// GPULayers is passed as -ngl when non-nil; nil keeps the llama.cpp default.
	GPULayers *int
	// MaxTokens bounds the generated answer (-n); 0 means 1024.
	MaxTokens int
	// MaxEdge bounds the longer frame edge in pixels; 0 means 768.
	MaxEdge int
	// CtxSize is passed as -c when > 0; 0 keeps the llama.cpp default.
	CtxSize int
}

func configFromOptions(opts vision.Options) (Config, error) {
	known := []string{OptionBinary, OptionCtxSize, OptionFFmpeg, OptionGPULayers, OptionMaxEdge, OptionMaxTokens, OptionMMProj, OptionModel, OptionThreads}
	var errs []error
	for key := range opts {
		if !slices.Contains(known, key) {
			errs = append(errs, fmt.Errorf("unknown option %q; supported: %s", key, strings.Join(known, ", ")))
		}
	}
	cfg := Config{
		Model:  opts[OptionModel],
		MMProj: opts[OptionMMProj],
		Binary: opts[OptionBinary],
		FFmpeg: opts[OptionFFmpeg],
	}
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
	intOption(OptionMaxEdge, 1, &cfg.MaxEdge)
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
		errs = append(errs, fmt.Errorf("option %q (path to a GGUF vision-language model) is required", OptionModel))
	}
	if strings.TrimSpace(c.MMProj) == "" {
		errs = append(errs, fmt.Errorf("option %q (path to the matching GGUF mmproj projector) is required", OptionMMProj))
	}
	if c.Threads < 0 || c.MaxTokens < 0 || c.MaxEdge < 0 || c.CtxSize < 0 || (c.GPULayers != nil && *c.GPULayers < 0) {
		errs = append(errs, errors.New("threads, gpu-layers, max-tokens, max-edge and ctx-size must not be negative"))
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	if c.Binary == "" {
		c.Binary = defaultBinary
	}
	if c.FFmpeg == "" {
		c.FFmpeg = defaultFFmpeg
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultMaxTokens
	}
	if c.MaxEdge == 0 {
		c.MaxEdge = defaultMaxEdge
	}
	return c, nil
}
