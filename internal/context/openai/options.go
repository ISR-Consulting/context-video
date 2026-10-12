package openai

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	contextcore "github.com/ISR-Consulting/context-video/internal/context"
)

// Option keys accepted by NewFromOptions.
const (
	OptionModel     = "model"
	OptionAPIKey    = "api-key"
	OptionBaseURL   = "base-url"
	OptionTimeout   = "timeout"
	OptionMaxTokens = "max-tokens"
)

const (
	defaultModel     = "gpt-4o-mini"
	defaultBaseURL   = "https://api.openai.com/v1"
	defaultTimeout   = 60 * time.Second
	defaultMaxTokens = 1024
	envAPIKey        = "OPENAI_API_KEY"
)

// Config configures an Adapter.
type Config struct {
	Model     string
	APIKey    string
	BaseURL   string
	Timeout   time.Duration
	MaxTokens int
}

func configFromOptions(opts contextcore.Options) (Config, error) {
	known := []string{OptionAPIKey, OptionBaseURL, OptionMaxTokens, OptionModel, OptionTimeout}
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
	}
	if raw, ok := opts[OptionTimeout]; ok {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("option %q must be a positive duration, got %q", OptionTimeout, raw))
		} else {
			cfg.Timeout = d
		}
	}
	if raw, ok := opts[OptionMaxTokens]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("option %q must be a positive integer, got %q", OptionMaxTokens, raw))
		} else {
			cfg.MaxTokens = n
		}
	}
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
	if c.Timeout < 0 || c.MaxTokens < 0 {
		return Config{}, errors.New("timeout and max-tokens must not be negative")
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultMaxTokens
	}
	return c, nil
}
