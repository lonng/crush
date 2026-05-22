package exports_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/exports"
)

func TestExternalConsumerCanCreateEmbeddedApp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	providers := exports.NewProviders()
	providers.Set("loop-test", exports.ProviderConfig{
		ID:      "loop-test",
		Name:    "Loop Test",
		Type:    exports.ProviderTypeOpenAICompat,
		BaseURL: "http://127.0.0.1:1/v1",
		Models:  []exports.Model{{ID: "test-large", Name: "Test Large", DefaultMaxTokens: 1024}},
	})

	cfg := exports.NewConfig()
	cfg.Providers = providers
	cfg.Models = map[exports.SelectedModelType]exports.SelectedModel{
		exports.SelectedModelTypeLarge: {Provider: "loop-test", Model: "test-large"},
		exports.SelectedModelTypeSmall: {Provider: "loop-test", Model: "test-large"},
	}

	app, err := exports.NewApp(
		ctx,
		t.TempDir(),
		"loop-agent",
		"",
		exports.WithConfig(cfg),
		exports.WithDataDir(filepath.Join(t.TempDir(), "data")),
	)
	if err != nil {
		t.Fatalf("NewApp returned error: %v", err)
	}
	defer app.Shutdown()

	if _, err := app.Sessions().Create(ctx, "external"); err != nil {
		t.Fatalf("create session through exported service: %v", err)
	}
}
