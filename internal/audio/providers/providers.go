// Package providers wires the concrete STT adapters into an audio.Registry. It
// is the only package that imports adapter implementations, so commands and
// pipelines select providers by name without depending on a vendor.
package providers

import (
	"github.com/ISR-Consulting/context-video/internal/audio"
	"github.com/ISR-Consulting/context-video/internal/audio/whispercpp"
)

// Default returns a new registry holding every built-in audio adapter.
func Default() *audio.Registry {
	r := audio.NewRegistry()
	if err := r.Register(whispercpp.ProviderName, whispercpp.NewFromOptions); err != nil {
		panic(err)
	}
	return r
}
