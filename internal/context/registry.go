package context

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Options are adapter-specific settings. Keys and values are opaque to callers
// and interpreted only by the selected Factory.
type Options map[string]string

// Factory builds a Reasoner from options.
type Factory func(Options) (Reasoner, error)

// ErrUnknownProvider reports a provider name with no registered factory.
var ErrUnknownProvider = errors.New("unknown context reasoning provider")

// Registry maps provider names to factories. The zero value is not usable; use
// NewRegistry. A Registry is not safe for concurrent registration.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a factory under name. Blank names, nil factories and duplicate
// names are rejected.
func (r *Registry) Register(name string, factory Factory) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("context: provider name must not be blank")
	}
	if factory == nil {
		return fmt.Errorf("context: nil factory for provider %q", name)
	}
	if _, ok := r.factories[name]; ok {
		return fmt.Errorf("context: provider %q already registered", name)
	}
	r.factories[name] = factory
	return nil
}

// Names returns the registered provider names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// New builds the reasoner registered under name. An unknown name returns an
// error wrapping ErrUnknownProvider that lists the available names.
func (r *Registry) New(name string, opts Options) (Reasoner, error) {
	factory, ok := r.factories[name]
	if !ok {
		available := strings.Join(r.Names(), ", ")
		if available == "" {
			available = "none"
		}
		return nil, fmt.Errorf("%w %q; available: %s", ErrUnknownProvider, name, available)
	}
	if opts == nil {
		opts = Options{}
	}
	reasoner, err := factory(opts)
	if err != nil {
		return nil, fmt.Errorf("context reasoning provider %q: %w", name, err)
	}
	if reasoner == nil {
		return nil, fmt.Errorf("context reasoning provider %q: factory returned nil reasoner", name)
	}
	return reasoner, nil
}
