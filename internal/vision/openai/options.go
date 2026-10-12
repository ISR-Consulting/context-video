package openai

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ISR-Consulting/context-video/internal/vision"
)

// Option keys accepted by NewFromOptions.
const (
	OptionModel     = "model"
	OptionAPIKey    = "api-key"
	OptionBaseURL   = "base-url"
	OptionTimeout   = "timeout"
	OptionFFmpeg    = "ffmpeg"
	OptionDetail    = "detail"
	OptionMaxTokens = "max-tokens"
	OptionMaxEdge   = "max-edge"
)

const (
	defaultModel     = "gpt-4o-mini"
	defaultBaseURL   = "https://api.openai.com/v1"
	defaultTimeout   = 60 * time.Second
	defaultFFmpeg    = "ffmpeg"
	defaultDetail    = "low"
	defaultMaxTokens = 1024
	defaultMaxEdge   = 768
	envAPIKey        = "OPENAI_API_KEY"
)

// Config configures an Adapter.
type Config struct {
	Model     string
	APIKey    string
	BaseURL   string
	Timeout   time.Duration
	FFmpeg    string
	Detail    string
	MaxTokens int
	MaxEdge   int
}

func configFromOptions(opts vision.Options) (Config, error) {
	known := []string{OptionAPIKey, OptionBaseURL, OptionDetail, OptionFFmpeg, OptionMaxEdge, OptionMaxTokens, OptionModel, OptionTimeout}
	var errs []error
	for key := range opts {
		if !slices.Contains(known, key) {
			errs = append(errs, fmt.Errorf("unknown option %q; supported: %s", key, strings.Join(known, ", ")))
		}
	}
	cfg := Config{
		Model:   opts[OptionModel],
		APIKey:  opts[OptionAPIKey],
		BaseURL: opts[OptionBaseURL],
		FFmpeg:  opts[OptionFFmpeg],
		Detail:  opts[OptionDetail],
	}
	if raw, ok := opts[OptionTimeout]; ok {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("option %q must be a positive duration, got %q", OptionTimeout, raw))
		} else {
			cfg.Timeout = d
		}
	}
	intOption := func(key string, min int, dst *int) {
		raw, ok := opts[key]
		if !ok {
			return
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < min {
			errs = append(errs, fmt.Errorf("option %q must be a positive integer, got %q", key, raw))
			return
		}
		*dst = n
	}
	intOption(OptionMaxTokens, 1, &cfg.MaxTokens)
	intOption(OptionMaxEdge, 1, &cfg.MaxEdge)
	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func (c Config) withDefaults() (Config, error) {
	if strings.TrimSpace(c.Model) == "" {
		c.Model = defaultModel
	}
	if strings.TrimSpace(c.APIKey) == "" {
		c.APIKey = strings.TrimSpace(os.Getenv(envAPIKey))
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return Config{}, fmt.Errorf("option %q or env %s is required", OptionAPIKey, envAPIKey)
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = defaultBaseURL
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.Timeout < 0 || c.MaxTokens < 0 || c.MaxEdge < 0 {
		return Config{}, errors.New("timeout, max-tokens and max-edge must not be negative")
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	if c.FFmpeg == "" {
		c.FFmpeg = defaultFFmpeg
	}
	if c.Detail == "" {
		c.Detail = defaultDetail
	}
	switch c.Detail {
	case "low", "high", "auto":
	default:
		return Config{}, fmt.Errorf("option %q must be low, high or auto, got %q", OptionDetail, c.Detail)
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultMaxTokens
	}
	if c.MaxEdge == 0 {
		c.MaxEdge = defaultMaxEdge
	}
	return c, nil
}
