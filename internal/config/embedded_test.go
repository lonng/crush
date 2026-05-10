package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
)

func TestLoadEmbeddedDoesNotReadOrWriteWorkingDirConfig(t *testing.T) {
	workingDir := t.TempDir()
	dataDir := t.TempDir()
	workingConfig := filepath.Join(workingDir, "crush.json")
	if err := os.WriteFile(workingConfig, []byte(`{not-json`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := LoadEmbedded(testEmbeddedConfig(), workingDir, dataDir, false)
	if err != nil {
		t.Fatalf("LoadEmbedded returned error: %v", err)
	}

	if got := store.Config().Options.DataDirectory; got != dataDir {
		t.Fatalf("data directory = %q, want %q", got, dataDir)
	}
	if got := store.LoadedPaths(); len(got) != 0 {
		t.Fatalf("loaded paths = %v, want none", got)
	}
	if data, err := os.ReadFile(workingConfig); err != nil {
		t.Fatalf("working config was removed: %v", err)
	} else if string(data) != `{not-json` {
		t.Fatalf("working config was modified: %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(workingDir, ".crush", "crush.json")); !os.IsNotExist(err) {
		t.Fatalf("LoadEmbedded wrote a workspace config, stat err=%v", err)
	}

	if err := store.ReloadFromDisk(context.Background()); err != nil {
		t.Fatalf("embedded ReloadFromDisk should be a no-op, got %v", err)
	}
}

func TestLoadEmbeddedClonesInputConfig(t *testing.T) {
	workingDir := t.TempDir()
	dataDir := t.TempDir()
	cfg := testEmbeddedConfig()

	store, err := LoadEmbedded(cfg, workingDir, dataDir, false)
	if err != nil {
		t.Fatalf("LoadEmbedded returned error: %v", err)
	}

	provider, ok := cfg.Providers.Get("loop-test")
	if !ok {
		t.Fatal("input config is missing test provider")
	}
	provider.BaseURL = "http://mutated.invalid/v1"
	cfg.Providers.Set("loop-test", provider)

	got, ok := store.Config().Providers.Get("loop-test")
	if !ok {
		t.Fatal("loaded config is missing test provider")
	}
	if got.BaseURL == provider.BaseURL {
		t.Fatalf("loaded config shared input provider map, got mutated base URL %q", got.BaseURL)
	}
}

func testEmbeddedConfig() *Config {
	return &Config{
		Options: &Options{
			DisableProviderAutoUpdate: true,
		},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{
			"loop-test": {
				ID:      "loop-test",
				Name:    "Loop Test",
				Type:    catwalk.TypeOpenAICompat,
				BaseURL: "http://127.0.0.1:1/v1",
				Models: []catwalk.Model{
					{
						ID:               "test-large",
						Name:             "Test Large",
						DefaultMaxTokens: 1024,
					},
				},
			},
		}),
		Models: map[SelectedModelType]SelectedModel{
			SelectedModelTypeLarge: {Provider: "loop-test", Model: "test-large"},
			SelectedModelTypeSmall: {Provider: "loop-test", Model: "test-large"},
		},
	}
}
