package adapter

import (
	"fmt"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[string]func() Adapter{}
)

// Register makes an adapter factory available under name. It returns an
// error if the name is empty, the factory is nil, or the name is taken.
func Register(name string, factory func() Adapter) error {
	if name == "" {
		return fmt.Errorf("adapter: register: empty name")
	}
	if factory == nil {
		return fmt.Errorf("adapter: register %q: nil factory", name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		return fmt.Errorf("adapter: register %q: already registered", name)
	}
	registry[name] = factory
	return nil
}

// Get returns the factory registered under name, or an error if none exists.
func Get(name string) (func() Adapter, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("adapter: unknown adapter %q", name)
	}
	return factory, nil
}
