package app

import (
	"context"
	"time"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// LSPEventType represents the type of LSP event
type LSPEventType string

const (
	LSPEventStateChanged       LSPEventType = "state_changed"
	LSPEventDiagnosticsChanged LSPEventType = "diagnostics_changed"
)

// LSPEvent represents an event in the LSP system
type LSPEvent struct {
	Type            LSPEventType
	Name            string
	State           lsp.ServerState
	Error           error
	DiagnosticCount int
}

// LSPClientInfo holds information about an LSP client's state
type LSPClientInfo struct {
	Name            string
	State           lsp.ServerState
	Error           error
	Client          *lsp.Client
	DiagnosticCount int
	ConnectedAt     time.Time
}

// LSPEventManager owns LSP event/state tracking for one app/workspace.
type LSPEventManager struct {
	states *csync.Map[string, LSPClientInfo]
	broker *pubsub.Broker[LSPEvent]
}

// NewLSPEventManager creates an isolated LSP event manager.
func NewLSPEventManager() *LSPEventManager {
	return &LSPEventManager{
		states: csync.NewMap[string, LSPClientInfo](),
		broker: pubsub.NewBroker[LSPEvent](),
	}
}

var defaultLSPEventManager = NewLSPEventManager()

// SubscribeLSPEvents returns a channel for LSP events
func SubscribeLSPEvents(ctx context.Context) <-chan pubsub.Event[LSPEvent] {
	return defaultLSPEventManager.SubscribeEvents(ctx)
}

// SubscribeEvents returns a channel for LSP events.
func (m *LSPEventManager) SubscribeEvents(ctx context.Context) <-chan pubsub.Event[LSPEvent] {
	return m.broker.Subscribe(ctx)
}

// GetLSPStates returns the current state of all LSP clients
func GetLSPStates() map[string]LSPClientInfo {
	return defaultLSPEventManager.GetStates()
}

// GetStates returns the current state of all LSP clients.
func (m *LSPEventManager) GetStates() map[string]LSPClientInfo {
	return m.states.Copy()
}

// GetLSPState returns the state of a specific LSP client
func GetLSPState(name string) (LSPClientInfo, bool) {
	return defaultLSPEventManager.GetState(name)
}

// GetState returns the state of a specific LSP client.
func (m *LSPEventManager) GetState(name string) (LSPClientInfo, bool) {
	return m.states.Get(name)
}

// updateLSPState updates the state of an LSP client and publishes an event
func updateLSPState(name string, state lsp.ServerState, err error, client *lsp.Client, diagnosticCount int) {
	defaultLSPEventManager.updateState(name, state, err, client, diagnosticCount)
}

func (m *LSPEventManager) updateState(name string, state lsp.ServerState, err error, client *lsp.Client, diagnosticCount int) {
	info := LSPClientInfo{
		Name:            name,
		State:           state,
		Error:           err,
		Client:          client,
		DiagnosticCount: diagnosticCount,
	}
	if state == lsp.StateReady {
		info.ConnectedAt = time.Now()
	} else if existing, ok := m.states.Get(name); ok {
		info.ConnectedAt = existing.ConnectedAt
	}
	m.states.Set(name, info)

	// Publish state change event
	m.broker.Publish(pubsub.UpdatedEvent, LSPEvent{
		Type:            LSPEventStateChanged,
		Name:            name,
		State:           state,
		Error:           err,
		DiagnosticCount: diagnosticCount,
	})
}

// updateLSPDiagnostics updates the diagnostic count for an LSP client and publishes an event
func updateLSPDiagnostics(name string, diagnosticCount int) {
	defaultLSPEventManager.updateDiagnostics(name, diagnosticCount)
}

func (m *LSPEventManager) updateDiagnostics(name string, diagnosticCount int) {
	if info, exists := m.states.Get(name); exists {
		info.DiagnosticCount = diagnosticCount
		m.states.Set(name, info)

		// Publish diagnostics change event
		m.broker.Publish(pubsub.UpdatedEvent, LSPEvent{
			Type:            LSPEventDiagnosticsChanged,
			Name:            name,
			State:           info.State,
			Error:           info.Error,
			DiagnosticCount: diagnosticCount,
		})
	}
}
