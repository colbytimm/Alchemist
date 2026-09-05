package adapter

import (
	"errors"
	"fmt"
	"sync"
)

// Registry errors, matchable with errors.Is.
var (
	ErrDuplicateName  = errors.New("name already registered")
	ErrUnknownAdapter = errors.New("unknown adapter")
)

// Factory constructs one adapter instance.
type Factory func() Adapter

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

func Register(name string, factory Factory) error {
	if name == "" {
		return errors.New("adapter: register: empty name")
	}
	if factory == nil {
		return fmt.Errorf("adapter: register %q: nil factory", name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		return fmt.Errorf("adapter: register %q: %w", name, ErrDuplicateName)
	}
	registry[name] = factory
	return nil
}

func Get(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("adapter: get %q: %w", name, ErrUnknownAdapter)
	}
	return factory, nil
}
