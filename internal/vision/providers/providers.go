// Package providers wires the concrete visual adapters into a vision.Registry.
// It is the only package that imports adapter implementations, so commands and
// pipelines select providers by name without depending on a vendor.
package providers

import (
	"github.com/ISR-Consulting/context-video/internal/vision"
	"github.com/ISR-Consulting/context-video/internal/vision/llamamtmd"
	"github.com/ISR-Consulting/context-video/internal/vision/openai"
)

// Default returns a new registry holding every built-in vision adapter.
func Default() *vision.Registry {
	r := vision.NewRegistry()
	if err := r.Register(llamamtmd.ProviderName, llamamtmd.NewFromOptions); err != nil {
		panic(err)
	}
	if err := r.Register(openai.ProviderName, openai.NewFromOptions); err != nil {
		panic(err)
	}
	return r
}
