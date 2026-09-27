package whispercpp

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ISR-Consulting/context-video/internal/audio"
)

// Option keys accepted by NewFromOptions.
const (
	OptionModel    = "model"
	OptionBinary   = "binary"
	OptionFFmpeg   = "ffmpeg"
	OptionLanguage = "language"
	OptionThreads  = "threads"
)

const (
	defaultBinary   = "whisper-cli"
	defaultFFmpeg   = "ffmpeg"
	defaultLanguage = "auto"
)

// Config configures an Adapter. Binary and FFmpeg are command names resolved
// through PATH or explicit paths.
type Config struct {
	Model    string
	Binary   string
	FFmpeg   string
	Language string
	// Threads is passed as -t when > 0; 0 keeps the whisper-cli default.
	Threads int
}

func configFromOptions(opts audio.Options) (Config, error) {
	known := []string{OptionBinary, OptionFFmpeg, OptionLanguage, OptionModel, OptionThreads}
	var errs []error
	for key := range opts {
		if !slices.Contains(known, key) {
			errs = append(errs, fmt.Errorf("unknown option %q; supported: %s", key, strings.Join(known, ", ")))
		}
	}
	cfg := Config{
		Model:    opts[OptionModel],
		Binary:   opts[OptionBinary],
		FFmpeg:   opts[OptionFFmpeg],
		Language: opts[OptionLanguage],
	}
	if raw, ok := opts[OptionThreads]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("option %q must be a positive integer, got %q", OptionThreads, raw))
		}
		cfg.Threads = n
	}
	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func (c Config) withDefaults() (Config, error) {
	if strings.TrimSpace(c.Model) == "" {
		return Config{}, fmt.Errorf("option %q (path to a ggml model file) is required", OptionModel)
	}
	if c.Binary == "" {
		c.Binary = defaultBinary
	}
	if c.FFmpeg == "" {
		c.FFmpeg = defaultFFmpeg
	}
	if c.Language == "" {
		c.Language = defaultLanguage
	}
	if c.Threads < 0 {
		return Config{}, fmt.Errorf("threads %d must not be negative", c.Threads)
	}
	return c, nil
}
