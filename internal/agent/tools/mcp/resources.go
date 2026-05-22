package mcp

import (
	"context"
	"errors"
	"iter"
	"log/slog"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Resource = mcp.Resource

type ResourceContents = mcp.ResourceContents

// Resources returns all available MCP resources from the default manager.
func Resources() iter.Seq2[string, []*Resource] {
	return defaultManager.Resources()
}

// Resources returns all available MCP resources.
func (m *Manager) Resources() iter.Seq2[string, []*Resource] {
	return m.allResources.Seq2()
}

// ListResources returns the current resources for an MCP server from the
// default manager.
func ListResources(ctx context.Context, cfg *config.ConfigStore, name string) ([]*Resource, error) {
	return defaultManager.ListResources(ctx, cfg, name)
}

// ListResources returns the current resources for an MCP server.
func (m *Manager) ListResources(ctx context.Context, cfg *config.ConfigStore, name string) ([]*Resource, error) {
	session, err := m.getOrRenewClient(ctx, cfg, name)
	if err != nil {
		return nil, err
	}

	resources, err := getResources(ctx, session)
	if err != nil {
		return nil, err
	}

	resourceCount := m.updateResources(name, resources)
	prev, _ := m.states.Get(name)
	prev.Counts.Resources = resourceCount
	m.updateState(name, StateConnected, nil, session, prev.Counts)
	return resources, nil
}

// ReadResource reads the contents of a resource from an MCP server in the
// default manager.
func ReadResource(ctx context.Context, cfg *config.ConfigStore, name, uri string) ([]*ResourceContents, error) {
	return defaultManager.ReadResource(ctx, cfg, name, uri)
}

// ReadResource reads the contents of a resource from an MCP server.
func (m *Manager) ReadResource(ctx context.Context, cfg *config.ConfigStore, name, uri string) ([]*ResourceContents, error) {
	session, err := m.getOrRenewClient(ctx, cfg, name)
	if err != nil {
		return nil, err
	}
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}
	return result.Contents, nil
}

// RefreshResources gets the updated list of resources from the MCP in the
// default manager and updates its state.
func RefreshResources(ctx context.Context, name string) {
	defaultManager.RefreshResources(ctx, name)
}

// RefreshResources gets the updated list of resources from the MCP and updates
// the manager state.
func (m *Manager) RefreshResources(ctx context.Context, name string) {
	session, ok := m.sessions.Get(name)
	if !ok {
		slog.Warn("Refresh resources: no session", "name", name)
		return
	}

	resources, err := getResources(ctx, session)
	if err != nil {
		m.updateState(name, StateError, err, nil, Counts{})
		return
	}

	resourceCount := m.updateResources(name, resources)

	prev, _ := m.states.Get(name)
	prev.Counts.Resources = resourceCount
	m.updateState(name, StateConnected, nil, session, prev.Counts)
}

func getResources(ctx context.Context, c *ClientSession) ([]*Resource, error) {
	if c.InitializeResult().Capabilities.Resources == nil {
		return nil, nil
	}
	result, err := c.ListResources(ctx, &mcp.ListResourcesParams{})
	if err != nil {
		// Handle "Method not found" errors from MCP servers that don't support resources/list.
		if isMethodNotFoundError(err) {
			slog.Warn("MCP server does not support resources/list", "error", err)
			return nil, nil
		}
		return nil, err
	}
	return result.Resources, nil
}

// isMethodNotFoundError checks if the error is a JSON-RPC "Method not found" error.
func isMethodNotFoundError(err error) bool {
	var rpcErr *jsonrpc.Error
	return errors.As(err, &rpcErr) && rpcErr != nil && rpcErr.Code == jsonrpc.CodeMethodNotFound
}

func (m *Manager) updateResources(name string, resources []*Resource) int {
	if len(resources) == 0 {
		m.allResources.Del(name)
		return 0
	}
	m.allResources.Set(name, resources)
	return len(resources)
}
