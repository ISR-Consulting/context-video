// Package providers wires the concrete context reasoning adapters into a
// context Registry. It is the only package that imports reasoner
// implementations, so commands and pipelines select a reasoner by name
// without depending on a vendor.
package providers

import (
	contextcore "github.com/ISR-Consulting/context-video/internal/context"
	"github.com/ISR-Consulting/context-video/internal/context/llamacpp"
)

// Default returns a new registry holding every built-in reasoning adapter.
func Default() *contextcore.Registry {
	r := contextcore.NewRegistry()
	if err := r.Register(llamacpp.ProviderName, llamacpp.NewFromOptions); err != nil {
		panic(err)
	}
	return r
}
