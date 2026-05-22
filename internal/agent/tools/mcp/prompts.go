package mcp

import (
	"context"
	"iter"
	"log/slog"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Prompt = mcp.Prompt

// Prompts returns all available MCP prompts from the default manager.
func Prompts() iter.Seq2[string, []*Prompt] {
	return defaultManager.Prompts()
}

// Prompts returns all available MCP prompts.
func (m *Manager) Prompts() iter.Seq2[string, []*Prompt] {
	return m.allPrompts.Seq2()
}

// GetPromptMessages retrieves the content of an MCP prompt with the given
// arguments from the default manager.
func GetPromptMessages(ctx context.Context, cfg *config.ConfigStore, clientName, promptName string, args map[string]string) ([]string, error) {
	return defaultManager.GetPromptMessages(ctx, cfg, clientName, promptName, args)
}

// GetPromptMessages retrieves the content of an MCP prompt with the given arguments.
func (m *Manager) GetPromptMessages(ctx context.Context, cfg *config.ConfigStore, clientName, promptName string, args map[string]string) ([]string, error) {
	c, err := m.getOrRenewClient(ctx, cfg, clientName)
	if err != nil {
		return nil, err
	}
	result, err := c.GetPrompt(ctx, &mcp.GetPromptParams{
		Name:      promptName,
		Arguments: args,
	})
	if err != nil {
		return nil, err
	}

	var messages []string
	for _, msg := range result.Messages {
		if msg.Role != "user" {
			continue
		}
		if textContent, ok := msg.Content.(*mcp.TextContent); ok {
			messages = append(messages, textContent.Text)
		}
	}
	return messages, nil
}

// RefreshPrompts gets the updated list of prompts from the MCP in the default
// manager and updates its state.
func RefreshPrompts(ctx context.Context, name string) {
	defaultManager.RefreshPrompts(ctx, name)
}

// RefreshPrompts gets the updated list of prompts from the MCP and updates the
// manager state.
func (m *Manager) RefreshPrompts(ctx context.Context, name string) {
	session, ok := m.sessions.Get(name)
	if !ok {
		slog.Warn("Refresh prompts: no session", "name", name)
		return
	}

	prompts, err := getPrompts(ctx, session)
	if err != nil {
		m.updateState(name, StateError, err, nil, Counts{})
		return
	}

	m.updatePrompts(name, prompts)

	prev, _ := m.states.Get(name)
	prev.Counts.Prompts = len(prompts)
	m.updateState(name, StateConnected, nil, session, prev.Counts)
}

func getPrompts(ctx context.Context, c *ClientSession) ([]*Prompt, error) {
	if c.InitializeResult().Capabilities.Prompts == nil {
		return nil, nil
	}
	result, err := c.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// updatePrompts updates the manager prompt maps.
func (m *Manager) updatePrompts(mcpName string, prompts []*Prompt) {
	if len(prompts) == 0 {
		m.allPrompts.Del(mcpName)
		return
	}
	m.allPrompts.Set(mcpName, prompts)
}
