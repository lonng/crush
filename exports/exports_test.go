package exports

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestProviders(t *testing.T) *Providers {
	t.Helper()
	providers := NewProviders()
	providers.Set("loop-test", ProviderConfig{
		ID:      "loop-test",
		Name:    "Loop Test",
		Type:    ProviderTypeOpenAICompat,
		BaseURL: "http://127.0.0.1:1/v1",
		Models:  []Model{{ID: "test-large", Name: "Test Large", DefaultMaxTokens: 1024}},
	})
	return providers
}

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	cfg := NewConfig()
	cfg.Providers = newTestProviders(t)
	cfg.Models = map[SelectedModelType]SelectedModel{
		SelectedModelTypeLarge: {Provider: "loop-test", Model: "test-large"},
		SelectedModelTypeSmall: {Provider: "loop-test", Model: "test-large"},
	}
	return cfg
}

func TestNewAppUsesEmbeddedConfigWithoutWritingWorkingDirConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workingDir := t.TempDir()

	app, err := NewApp(ctx, workingDir, "loop-agent", "", WithConfig(newTestConfig(t)))
	if err != nil {
		t.Fatalf("NewApp returned error: %v", err)
	}
	defer app.Shutdown()

	if got := app.ID(); got != "loop-agent" {
		t.Fatalf("app ID = %q, want loop-agent", got)
	}
	if _, err := os.Stat(filepath.Join(workingDir, "loop.json")); !os.IsNotExist(err) {
		t.Fatalf("NewApp wrote workingDir/loop.json, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workingDir, ".loop", "loop.json")); !os.IsNotExist(err) {
		t.Fatalf("NewApp wrote workingDir/.loop/loop.json, stat err=%v", err)
	}
}

func TestCurrentSessionIDTracksCreatedAndResumedSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workingDir := t.TempDir()
	cfg := newTestConfig(t)

	app, err := NewApp(ctx, workingDir, "loop-agent", "", WithConfig(cfg))
	if err != nil {
		t.Fatalf("NewApp returned error: %v", err)
	}
	sess, err := app.Sessions().Create(ctx, "first")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if got := app.CurrentSessionID(); got != sess.ID {
		t.Fatalf("current session after create = %q, want %q", got, sess.ID)
	}
	app.Shutdown()

	resumed, err := NewApp(ctx, workingDir, "loop-agent", sess.ID, WithConfig(cfg))
	if err != nil {
		t.Fatalf("NewApp for resume returned error: %v", err)
	}
	defer resumed.Shutdown()

	got, err := resumed.Sessions().Create(ctx, "ignored")
	if err != nil {
		t.Fatalf("resume session through Create: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("resumed session ID = %q, want %q", got.ID, sess.ID)
	}
	if current := resumed.CurrentSessionID(); current != sess.ID {
		t.Fatalf("current session after resume = %q, want %q", current, sess.ID)
	}
}

func TestSubscribeSessionsReturnsExportedEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app, err := NewApp(ctx, t.TempDir(), "loop-agent", "", WithConfig(newTestConfig(t)))
	if err != nil {
		t.Fatalf("NewApp returned error: %v", err)
	}
	defer app.Shutdown()

	events := app.SubscribeSessions(ctx)
	sess, err := app.Sessions().Create(ctx, "evented")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	select {
	case event := <-events:
		if event.Type != EventCreated {
			t.Fatalf("event type = %q, want %q", event.Type, EventCreated)
		}
		if event.Session.ID != sess.ID {
			t.Fatalf("event session ID = %q, want %q", event.Session.ID, sess.ID)
		}
	case <-ctx.Done():
		t.Fatal("context canceled before session event")
	}
}
