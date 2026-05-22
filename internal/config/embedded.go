package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/env"
)

// LoadEmbedded builds a ConfigStore from an in-memory Config.
//
// This is intended for in-process hosts that embed Crush as a library. Unlike
// Load, it does not discover or merge crush.json files from the working
// directory, the user's global config, or the workspace data directory. The
// returned store keeps all runtime-only behavior in memory and disables disk
// reloads so hosts can control configuration explicitly.
func LoadEmbedded(cfg *Config, workingDir, dataDir string, debug bool) (*ConfigStore, error) {
	cloned, err := cloneConfig(cfg)
	if err != nil {
		return nil, err
	}

	cloned.setDefaults(workingDir, dataDir)
	if debug {
		cloned.Options.Debug = true
	}

	configPath := filepath.Join(cloned.Options.DataDirectory, fmt.Sprintf("%s.json", appName))
	store := &ConfigStore{
		config:         cloned,
		workingDir:     workingDir,
		globalDataPath: configPath,
		workspacePath:  configPath,
		embedded:       true,
	}

	// Validate hooks after defaults have been applied, mirroring Load.
	if err := cloned.ValidateHooks(); err != nil {
		return nil, fmt.Errorf("invalid hook configuration: %w", err)
	}

	if !isInsideWorktree() {
		const depth = 2
		const items = 100
		slog.Warn("No git repository detected in working directory, will limit file walk operations", "depth", depth, "items", items)
		assignIfNil(&cloned.Tools.Ls.MaxDepth, depth)
		assignIfNil(&cloned.Tools.Ls.MaxItems, items)
		assignIfNil(&cloned.Options.TUI.Completions.MaxDepth, depth)
		assignIfNil(&cloned.Options.TUI.Completions.MaxItems, items)
	}

	if isAppleTerminal() {
		slog.Warn("Detected Apple Terminal, enabling transparent mode")
		assignIfNil(&cloned.Options.TUI.Transparent, true)
	}

	providers, err := Providers(cloned)
	if err != nil {
		return nil, err
	}
	store.knownProviders = providers

	env := env.New()
	resolver := NewShellVariableResolver(env)
	store.resolver = resolver

	store.autoReloadDisabled = true
	defer func() { store.autoReloadDisabled = false }()

	if err := cloned.configureProviders(store, env, resolver, store.knownProviders); err != nil {
		return nil, fmt.Errorf("failed to configure providers: %w", err)
	}

	if !cloned.IsConfigured() {
		slog.Warn("No providers configured")
		store.captureStalenessSnapshot(nil)
		return store, nil
	}

	// Embedded stores must not mutate the host's global Crush config when a
	// requested model is invalid; fall back in memory instead.
	if err := configureSelectedModels(store, store.knownProviders, false); err != nil {
		return nil, fmt.Errorf("failed to configure selected models: %w", err)
	}
	store.SetupAgents()

	store.captureStalenessSnapshot(nil)
	return store, nil
}

func cloneConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		return &Config{}, nil
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedded config: %w", err)
	}
	cloned, err := loadFromBytes([][]byte{data})
	if err != nil {
		return nil, fmt.Errorf("failed to clone embedded config: %w", err)
	}
	return cloned, nil
}
