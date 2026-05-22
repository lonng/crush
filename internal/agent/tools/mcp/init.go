// Package mcp provides functionality for managing Model Context Protocol (MCP)
// clients within the Crush application.
package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func parseLevel(level mcp.LoggingLevel) slog.Level {
	switch level {
	case "info":
		return slog.LevelInfo
	case "notice":
		return slog.LevelInfo
	case "warning":
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}

// ClientSession wraps an mcp.ClientSession with a context cancel function so
// that the context created during session establishment is properly cleaned up
// on close.
type ClientSession struct {
	*mcp.ClientSession
	cancel context.CancelFunc
}

// Close cancels the session context and then closes the underlying session.
func (s *ClientSession) Close() error {
	s.cancel()
	return s.ClientSession.Close()
}

// Manager owns MCP client state for one Crush app/workspace instance.
//
// Historically this package kept sessions, state, prompts, resources, tools, and
// events in package globals. That makes the CLI simple, but it also prevents
// embedding Crush multiple times in one process because independent app
// instances would share MCP clients. Manager keeps the same behavior behind an
// explicit object while the package-level functions below delegate to a default
// manager for existing callers.
type Manager struct {
	sessions     *csync.Map[string, *ClientSession]
	states       *csync.Map[string, ClientInfo]
	allTools     *csync.Map[string, []*Tool]
	allPrompts   *csync.Map[string, []*Prompt]
	allResources *csync.Map[string, []*Resource]
	broker       *pubsub.Broker[Event]
	initOnce     sync.Once
	initDone     chan struct{}
}

// NewManager creates an isolated MCP manager. Use one Manager per embedded
// Crush app/workspace.
func NewManager() *Manager {
	return &Manager{
		sessions:     csync.NewMap[string, *ClientSession](),
		states:       csync.NewMap[string, ClientInfo](),
		allTools:     csync.NewMap[string, []*Tool](),
		allPrompts:   csync.NewMap[string, []*Prompt](),
		allResources: csync.NewMap[string, []*Resource](),
		broker:       pubsub.NewBroker[Event](),
		initDone:     make(chan struct{}),
	}
}

var defaultManager = NewManager()

// DefaultManager returns the package-level singleton used by legacy callers.
func DefaultManager() *Manager {
	return defaultManager
}

// State represents the current state of an MCP client
type State int

const (
	StateDisabled State = iota
	StateStarting
	StateConnected
	StateError
)

func (s State) String() string {
	switch s {
	case StateDisabled:
		return "disabled"
	case StateStarting:
		return "starting"
	case StateConnected:
		return "connected"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// EventType represents the type of MCP event
type EventType uint

const (
	EventStateChanged EventType = iota
	EventToolsListChanged
	EventPromptsListChanged
	EventResourcesListChanged
)

// Event represents an event in the MCP system
type Event struct {
	Type   EventType
	Name   string
	State  State
	Error  error
	Counts Counts
}

// Counts number of available tools, prompts, etc.
type Counts struct {
	Tools     int
	Prompts   int
	Resources int
}

// ClientInfo holds information about an MCP client's state
type ClientInfo struct {
	Name        string
	State       State
	Error       error
	Client      *ClientSession
	Counts      Counts
	ConnectedAt time.Time
}

// SubscribeEvents returns a channel for MCP events from the default manager.
func SubscribeEvents(ctx context.Context) <-chan pubsub.Event[Event] {
	return defaultManager.SubscribeEvents(ctx)
}

// SubscribeEvents returns a channel for MCP events.
func (m *Manager) SubscribeEvents(ctx context.Context) <-chan pubsub.Event[Event] {
	return m.broker.Subscribe(ctx)
}

// GetStates returns the current state of all MCP clients from the default manager.
func GetStates() map[string]ClientInfo {
	return defaultManager.GetStates()
}

// GetStates returns the current state of all MCP clients.
func (m *Manager) GetStates() map[string]ClientInfo {
	return m.states.Copy()
}

// GetState returns the state of a specific MCP client from the default manager.
func GetState(name string) (ClientInfo, bool) {
	return defaultManager.GetState(name)
}

// GetState returns the state of a specific MCP client.
func (m *Manager) GetState(name string) (ClientInfo, bool) {
	return m.states.Get(name)
}

// Close closes all MCP clients in the default manager. This should be called during application shutdown.
func Close(ctx context.Context) error {
	return defaultManager.Close(ctx)
}

// Close closes all MCP clients. This should be called during application shutdown.
func (m *Manager) Close(ctx context.Context) error {
	var wg sync.WaitGroup
	for name, session := range m.sessions.Seq2() {
		wg.Go(func() {
			done := make(chan error, 1)
			go func() {
				done <- session.Close()
			}()
			select {
			case err := <-done:
				if err != nil &&
					!errors.Is(err, io.EOF) &&
					!errors.Is(err, context.Canceled) &&
					err.Error() != "signal: killed" {
					slog.Warn("Failed to shutdown MCP client", "name", name, "error", err)
				}
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
	m.broker.Shutdown()
	return nil
}

// Initialize initializes MCP clients in the default manager based on the provided configuration.
func Initialize(ctx context.Context, permissions permission.Service, cfg *config.ConfigStore) {
	defaultManager.Initialize(ctx, permissions, cfg)
}

// Initialize initializes MCP clients based on the provided configuration.
func (m *Manager) Initialize(ctx context.Context, permissions permission.Service, cfg *config.ConfigStore) {
	slog.Info("Initializing MCP clients")
	var wg sync.WaitGroup
	// Initialize states for all configured MCPs
	for name, mcpCfg := range cfg.Config().MCP {
		if mcpCfg.Disabled {
			m.updateState(name, StateDisabled, nil, nil, Counts{})
			slog.Debug("Skipping disabled MCP", "name", name)
			continue
		}

		// Set initial starting state
		wg.Add(1)
		go func(name string, mcpCfg config.MCPConfig) {
			defer func() {
				wg.Done()
				if r := recover(); r != nil {
					var err error
					switch v := r.(type) {
					case error:
						err = v
					case string:
						err = fmt.Errorf("panic: %s", v)
					default:
						err = fmt.Errorf("panic: %v", v)
					}
					m.updateState(name, StateError, err, nil, Counts{})
					slog.Error("Panic in MCP client initialization", "error", err, "name", name)
				}
			}()

			if err := m.initClient(ctx, cfg, name, mcpCfg, cfg.Resolver()); err != nil {
				slog.Debug("Failed to initialize MCP client", "name", name, "error", err)
			}
		}(name, mcpCfg)
	}
	wg.Wait()
	m.initOnce.Do(func() { close(m.initDone) })
}

// WaitForInit blocks until MCP initialization is complete in the default manager.
// If Initialize was never called, this blocks until the context is cancelled.
func WaitForInit(ctx context.Context) error {
	return defaultManager.WaitForInit(ctx)
}

// WaitForInit blocks until MCP initialization is complete.
// If Initialize was never called, this blocks until the context is cancelled.
func (m *Manager) WaitForInit(ctx context.Context) error {
	select {
	case <-m.initDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InitializeSingle initializes a single MCP client by name in the default manager.
func InitializeSingle(ctx context.Context, name string, cfg *config.ConfigStore) error {
	return defaultManager.InitializeSingle(ctx, name, cfg)
}

// InitializeSingle initializes a single MCP client by name.
func (m *Manager) InitializeSingle(ctx context.Context, name string, cfg *config.ConfigStore) error {
	mcpCfg, exists := cfg.Config().MCP[name]
	if !exists {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}

	if mcpCfg.Disabled {
		m.updateState(name, StateDisabled, nil, nil, Counts{})
		slog.Debug("Skipping disabled MCP", "name", name)
		return nil
	}

	return m.initClient(ctx, cfg, name, mcpCfg, cfg.Resolver())
}

// initClient initializes a single MCP client with the given configuration.
func (m *Manager) initClient(ctx context.Context, cfg *config.ConfigStore, name string, mcpCfg config.MCPConfig, resolver config.VariableResolver) error {
	// Set initial starting state.
	m.updateState(name, StateStarting, nil, nil, Counts{})

	// createSession handles its own timeout internally.
	session, err := m.createSession(ctx, name, mcpCfg, resolver)
	if err != nil {
		return err
	}

	tools, err := getTools(ctx, session)
	if err != nil {
		slog.Error("Error listing tools", "error", err)
		m.updateState(name, StateError, err, nil, Counts{})
		session.Close()
		return err
	}

	prompts, err := getPrompts(ctx, session)
	if err != nil {
		slog.Error("Error listing prompts", "error", err)
		m.updateState(name, StateError, err, nil, Counts{})
		session.Close()
		return err
	}

	toolCount := m.updateTools(cfg, name, tools)
	m.updatePrompts(name, prompts)
	m.sessions.Set(name, session)

	m.updateState(name, StateConnected, nil, session, Counts{
		Tools:   toolCount,
		Prompts: len(prompts),
	})

	return nil
}

// DisableSingle disables and closes a single MCP client by name in the default manager.
func DisableSingle(cfg *config.ConfigStore, name string) error {
	return defaultManager.DisableSingle(cfg, name)
}

// DisableSingle disables and closes a single MCP client by name.
func (m *Manager) DisableSingle(cfg *config.ConfigStore, name string) error {
	session, ok := m.sessions.Get(name)
	if ok {
		if err := session.Close(); err != nil &&
			!errors.Is(err, io.EOF) &&
			!errors.Is(err, context.Canceled) &&
			err.Error() != "signal: killed" {
			slog.Warn("Error closing MCP session", "name", name, "error", err)
		}
		m.sessions.Del(name)
	}

	// Clear tools and prompts for this MCP.
	m.updateTools(cfg, name, nil)
	m.updatePrompts(name, nil)

	// Update state to disabled.
	m.updateState(name, StateDisabled, nil, nil, Counts{})

	slog.Info("Disabled mcp client", "name", name)
	return nil
}

func (m *Manager) getOrRenewClient(ctx context.Context, cfg *config.ConfigStore, name string) (*ClientSession, error) {
	sess, ok := m.sessions.Get(name)
	if !ok {
		return nil, fmt.Errorf("mcp '%s' not available", name)
	}

	mcpCfg := cfg.Config().MCP[name]
	state, _ := m.states.Get(name)

	timeout := mcpTimeout(mcpCfg)
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := sess.Ping(pingCtx, nil)
	if err == nil {
		return sess, nil
	}
	m.updateState(name, StateError, maybeTimeoutErr(err, timeout), nil, state.Counts)

	sess, err = m.createSession(ctx, name, mcpCfg, cfg.Resolver())
	if err != nil {
		return nil, err
	}

	m.updateState(name, StateConnected, nil, sess, state.Counts)
	m.sessions.Set(name, sess)
	return sess, nil
}

// updateState updates the state of an MCP client and publishes an event.
func (m *Manager) updateState(name string, state State, err error, client *ClientSession, counts Counts) {
	info := ClientInfo{
		Name:   name,
		State:  state,
		Error:  err,
		Client: client,
		Counts: counts,
	}
	switch state {
	case StateConnected:
		info.ConnectedAt = time.Now()
	case StateError:
		m.sessions.Del(name)
	}
	m.states.Set(name, info)

	// Publish state change event
	m.broker.Publish(pubsub.UpdatedEvent, Event{
		Type:   EventStateChanged,
		Name:   name,
		State:  state,
		Error:  err,
		Counts: counts,
	})
}

func (m *Manager) createSession(ctx context.Context, name string, mcpCfg config.MCPConfig, resolver config.VariableResolver) (*ClientSession, error) {
	timeout := mcpTimeout(mcpCfg)
	mcpCtx, cancel := context.WithCancel(ctx)
	cancelTimer := time.AfterFunc(timeout, cancel)

	transport, err := createTransport(mcpCtx, mcpCfg, resolver)
	if err != nil {
		m.updateState(name, StateError, err, nil, Counts{})
		slog.Error("Error creating MCP client", "error", err, "name", name)
		cancel()
		cancelTimer.Stop()
		return nil, err
	}

	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "crush",
			Version: version.Version,
			Title:   "Crush",
		},
		&mcp.ClientOptions{
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
				m.broker.Publish(pubsub.UpdatedEvent, Event{
					Type: EventToolsListChanged,
					Name: name,
				})
			},
			PromptListChangedHandler: func(context.Context, *mcp.PromptListChangedRequest) {
				m.broker.Publish(pubsub.UpdatedEvent, Event{
					Type: EventPromptsListChanged,
					Name: name,
				})
			},
			ResourceListChangedHandler: func(context.Context, *mcp.ResourceListChangedRequest) {
				m.broker.Publish(pubsub.UpdatedEvent, Event{
					Type: EventResourcesListChanged,
					Name: name,
				})
			},
			LoggingMessageHandler: func(ctx context.Context, req *mcp.LoggingMessageRequest) {
				level := parseLevel(req.Params.Level)
				slog.Log(ctx, level, "MCP log", "name", name, "logger", req.Params.Logger, "data", req.Params.Data)
			},
		},
	)

	session, err := client.Connect(mcpCtx, transport, nil)
	if err != nil {
		err = maybeStdioErr(err, transport)
		m.updateState(name, StateError, maybeTimeoutErr(err, timeout), nil, Counts{})
		slog.Error("MCP client failed to initialize", "error", err, "name", name)
		cancel()
		cancelTimer.Stop()
		return nil, err
	}

	cancelTimer.Stop()
	slog.Debug("MCP client initialized", "name", name)
	return &ClientSession{session, cancel}, nil
}

// maybeStdioErr if a stdio mcp prints an error in non-json format, it'll fail
// to parse, and the cli will then close it, causing the EOF error.
// so, if we got an EOF err, and the transport is STDIO, we try to exec it
// again with a timeout and collect the output so we can add details to the
// error.
// this happens particularly when starting things with npx, e.g. if node can't
// be found or some other error like that.
func maybeStdioErr(err error, transport mcp.Transport) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	ct, ok := transport.(*mcp.CommandTransport)
	if !ok {
		return err
	}
	if err2 := stdioCheck(ct.Command); err2 != nil {
		err = errors.Join(err, err2)
	}
	return err
}

func maybeTimeoutErr(err error, timeout time.Duration) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}

func createTransport(ctx context.Context, m config.MCPConfig, resolver config.VariableResolver) (mcp.Transport, error) {
	switch m.Type {
	case config.MCPStdio:
		command, err := resolver.ResolveValue(m.Command)
		if err != nil {
			return nil, fmt.Errorf("invalid mcp command: %w", err)
		}
		if strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("mcp stdio config requires a non-empty 'command' field")
		}
		args, err := m.ResolvedArgs(resolver)
		if err != nil {
			return nil, err
		}
		envs, err := m.ResolvedEnv(resolver)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, home.Long(command), args...)
		if cwd := strings.TrimSpace(m.CWD); cwd != "" {
			resolvedCWD, err := resolver.ResolveValue(cwd)
			if err != nil {
				return nil, fmt.Errorf("cwd: %w", err)
			}
			resolvedCWD = strings.TrimSpace(resolvedCWD)
			if resolvedCWD != "" {
				cmd.Dir = home.Long(resolvedCWD)
			}
		}
		cmd.Env = append(os.Environ(), envs...)
		return &mcp.CommandTransport{
			Command: cmd,
		}, nil
	case config.MCPHttp:
		url, err := m.ResolvedURL(resolver)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("mcp http config requires a non-empty 'url' field")
		}
		headers, err := m.ResolvedHeaders(resolver)
		if err != nil {
			return nil, err
		}
		client := &http.Client{
			Transport: &headerRoundTripper{
				headers: headers,
			},
		}
		return &mcp.StreamableClientTransport{
			Endpoint:   url,
			HTTPClient: client,
		}, nil
	case config.MCPSSE:
		url, err := m.ResolvedURL(resolver)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("mcp sse config requires a non-empty 'url' field")
		}
		headers, err := m.ResolvedHeaders(resolver)
		if err != nil {
			return nil, err
		}
		client := &http.Client{
			Transport: &headerRoundTripper{
				headers: headers,
			},
		}
		return &mcp.SSEClientTransport{
			Endpoint:   url,
			HTTPClient: client,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported mcp type: %s", m.Type)
	}
}

type headerRoundTripper struct {
	headers map[string]string
}

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range rt.headers {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func mcpTimeout(m config.MCPConfig) time.Duration {
	return time.Duration(cmp.Or(m.Timeout, 15)) * time.Second
}

func stdioCheck(old *exec.Cmd) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	cmd := exec.CommandContext(ctx, old.Path, old.Args...)
	cmd.Env = old.Env
	out, err := cmd.CombinedOutput()
	if err == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return fmt.Errorf("%w: %s", err, string(out))
}
