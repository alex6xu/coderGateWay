package platform

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// PlatformManager manages all platform adapters
type PlatformManager struct {
	adapters map[string]PlatformAdapter
	mu       sync.RWMutex
	handler  MessageHandler
}

// NewPlatformManager creates a new platform manager
func NewPlatformManager() *PlatformManager {
	return &PlatformManager{
		adapters: make(map[string]PlatformAdapter),
	}
}

// Register registers a platform adapter
func (m *PlatformManager) Register(adapter PlatformAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.adapters[adapter.Name()] = adapter

	// Set handler if already registered
	if m.handler != nil {
		adapter.OnMessage(m.handler)
	}
}

// OnMessage registers a message handler for all adapters
func (m *PlatformManager) OnMessage(handler MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.handler = handler

	for _, adapter := range m.adapters {
		adapter.OnMessage(handler)
	}
}

// Start starts all platform adapters
func (m *PlatformManager) Start(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, adapter := range m.adapters {
		log.Printf("Starting platform: %s", name)
		if err := adapter.Start(ctx); err != nil {
			return fmt.Errorf("failed to start platform %s: %w", name, err)
		}
	}

	return nil
}

// Stop stops all platform adapters
func (m *PlatformManager) Stop() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, adapter := range m.adapters {
		log.Printf("Stopping platform: %s", name)
		if err := adapter.Stop(); err != nil {
			log.Printf("Failed to stop platform %s: %v", name, err)
		}
	}

	return nil
}

// SendMessage sends a message to a specific platform
func (m *PlatformManager) SendMessage(platform string, msg *Message) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapter, ok := m.adapters[platform]
	if !ok {
		return fmt.Errorf("platform not found: %s", platform)
	}

	return adapter.SendMessage(msg)
}

// BroadcastMessage broadcasts a message to all platforms
func (m *PlatformManager) BroadcastMessage(msg *Message) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, adapter := range m.adapters {
		if err := adapter.SendMessage(msg); err != nil {
			log.Printf("Failed to send message to %s: %v", adapter.Name(), err)
		}
	}

	return nil
}

// GetAdapter returns a platform adapter by name
func (m *PlatformManager) GetAdapter(name string) (PlatformAdapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapter, ok := m.adapters[name]
	if !ok {
		return nil, fmt.Errorf("platform not found: %s", name)
	}

	return adapter, nil
}

// ListAdapters returns all registered adapters
func (m *PlatformManager) ListAdapters() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.adapters))
	for name := range m.adapters {
		names = append(names, name)
	}

	return names
}
