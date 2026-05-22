// Package exports exposes the small, supported surface that external hosts
// need when embedding Crush in-process.
package exports

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

const defaultSchema = "https://charm.land/crush.json"

// Re-exported helpers that embedded hosts commonly need.
var (
	SetupLog             = log.Setup
	NewProviders         = csync.NewMap[string, ProviderConfig]
	NewProvidersFrom     = csync.NewMapFrom[string, ProviderConfig]
	Assistant            = message.Assistant
	User                 = message.User
	System               = message.System
	Tool                 = message.Tool
	DiscoverSkills       = skills.Discover
	DiscoverSkillsStates = skills.DiscoverWithStates
	ParseSkill           = skills.Parse
	ParseSkillContent    = skills.ParseContent
)

const (
	FinishReasonEndTurn   = message.FinishReasonEndTurn
	FinishReasonMaxTokens = message.FinishReasonMaxTokens
	FinishReasonToolUse   = message.FinishReasonToolUse
	FinishReasonCanceled  = message.FinishReasonCanceled
	FinishReasonError     = message.FinishReasonError
	FinishReasonUnknown   = message.FinishReasonUnknown

	SelectedModelTypeLarge = config.SelectedModelTypeLarge
	SelectedModelTypeSmall = config.SelectedModelTypeSmall

	MCPStdio = config.MCPStdio
	MCPSSE   = config.MCPSSE
	MCPHttp  = config.MCPHttp

	ProviderTypeOpenAI       = catwalk.TypeOpenAI
	ProviderTypeOpenAICompat = catwalk.TypeOpenAICompat
	ProviderTypeOpenRouter   = catwalk.TypeOpenRouter
	ProviderTypeVercel       = catwalk.TypeVercel
	ProviderTypeAnthropic    = catwalk.TypeAnthropic
	ProviderTypeGoogle       = catwalk.TypeGoogle
	ProviderTypeAzure        = catwalk.TypeAzure
	ProviderTypeBedrock      = catwalk.TypeBedrock
	ProviderTypeVertexAI     = catwalk.TypeVertexAI
)

type (
	Config            = config.Config
	Options           = config.Options
	TUIOptions        = config.TUIOptions
	Completions       = config.Completions
	Permissions       = config.Permissions
	ProviderConfig    = config.ProviderConfig
	Providers         = csync.Map[string, ProviderConfig]
	Model             = catwalk.Model
	InferenceProvider = catwalk.InferenceProvider
	ProviderType      = catwalk.Type
	SelectedModel     = config.SelectedModel
	SelectedModelType = config.SelectedModelType

	MCPs      = config.MCPs
	MCPType   = config.MCPType
	MCPConfig = config.MCPConfig
	LSPs      = config.LSPs
	LSPConfig = config.LSPConfig

	Message               = message.Message
	CreateMessageParams   = message.CreateMessageParams
	MessageRole           = message.MessageRole
	MessageService        = message.Service
	ContentPart           = message.ContentPart
	ContentPartReasoning  = message.ReasoningContent
	ContentPartText       = message.TextContent
	ContentPartImageURL   = message.ImageURLContent
	ContentPartBinary     = message.BinaryContent
	ContentPartToolCall   = message.ToolCall
	ContentPartToolResult = message.ToolResult
	ContentPartFinish     = message.Finish
	FinishReason          = message.FinishReason

	Session        = session.Session
	SessionService = session.Service
	Todo           = session.Todo
	TodoStatus     = session.TodoStatus

	Skill      = skills.Skill
	SkillState = skills.SkillState
)

// EventType identifies the lifecycle event for exported message/session
// subscriptions.
type EventType string

const (
	EventCreated EventType = "created"
	EventUpdated EventType = "updated"
	EventDeleted EventType = "deleted"
)

// MessageEvent is a public wrapper around Crush message service events.
type MessageEvent struct {
	Type    EventType
	Message Message
}

// SessionEvent is a public wrapper around Crush session service events.
type SessionEvent struct {
	Type    EventType
	Session Session
}

// App wraps an internal Crush app instance.
type App struct {
	internal *app.App
	id       string
	sessions *sessionService
}

// ID returns the caller-supplied identifier for this embedded app instance.
func (a *App) ID() string {
	if a == nil {
		return ""
	}
	return a.id
}

// Config returns the resolved in-memory configuration.
func (a *App) Config() *Config {
	return a.internal.Config()
}

// Messages returns the message service. Prefer SubscribeMessages when the
// caller needs event streaming without importing Crush internals.
func (a *App) Messages() MessageService {
	return a.internal.Messages
}

// Sessions returns the session service. Prefer SubscribeSessions when the
// caller needs event streaming without importing Crush internals.
func (a *App) Sessions() SessionService {
	return a.internal.Sessions
}

// CurrentSessionID returns the top-level session most recently selected by
// RunWithOptions/Run.
func (a *App) CurrentSessionID() string {
	if a == nil || a.sessions == nil {
		return ""
	}
	return a.sessions.CurrentSessionID()
}

// SubscribeMessages converts internal message events to exported events.
func (a *App) SubscribeMessages(ctx context.Context) <-chan MessageEvent {
	out := make(chan MessageEvent, 64)
	if a == nil || a.internal == nil {
		close(out)
		return out
	}
	in := a.internal.Messages.Subscribe(ctx)
	go func() {
		defer close(out)
		for event := range in {
			select {
			case out <- MessageEvent{Type: EventType(event.Type), Message: event.Payload}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// SubscribeSessions converts internal session events to exported events.
func (a *App) SubscribeSessions(ctx context.Context) <-chan SessionEvent {
	out := make(chan SessionEvent, 64)
	if a == nil || a.internal == nil {
		close(out)
		return out
	}
	in := a.internal.Sessions.Subscribe(ctx)
	go func() {
		defer close(out)
		for event := range in {
			select {
			case out <- SessionEvent{Type: EventType(event.Type), Session: event.Payload}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Shutdown performs graceful shutdown for the embedded app.
func (a *App) Shutdown() {
	if a != nil && a.internal != nil {
		a.internal.Shutdown()
	}
}

// Run keeps compatibility with the Qingma embedding wrapper. It executes a
// non-interactive run with spinner/progress hidden when quiet is true.
func (a *App) Run(ctx context.Context, output io.Writer, prompt string, quiet bool) error {
	return a.RunWithOptions(ctx, output, prompt, RunOptions{HideSpinner: quiet})
}

// RunOptions controls a non-interactive embedded run.
type RunOptions struct {
	LargeModel        string
	SmallModel        string
	HideSpinner       bool
	ContinueSessionID string
	UseLast           bool
}

// RunWithOptions executes Crush in non-interactive mode.
func (a *App) RunWithOptions(ctx context.Context, output io.Writer, prompt string, opts RunOptions) error {
	if a == nil || a.internal == nil {
		return fmt.Errorf("crush app is nil")
	}
	if opts.ContinueSessionID != "" {
		a.sessions.SetCurrentSessionID(opts.ContinueSessionID)
	}
	return a.internal.RunNonInteractive(
		ctx,
		output,
		prompt,
		opts.LargeModel,
		opts.SmallModel,
		opts.HideSpinner,
		opts.ContinueSessionID,
		opts.UseLast,
	)
}

// Option configures NewApp.
type Option func(*appOptions)

type appOptions struct {
	config                 *Config
	dataDir                string
	debug                  bool
	skipPermissionRequests bool
	disableUpdateCheck     bool
}

// WithConfig replaces the default embedded config. The config is cloned during
// loading so later caller-side mutation does not change the running app.
func WithConfig(cfg *Config) Option {
	return func(opts *appOptions) {
		opts.config = cfg
	}
}

// WithDataDir sets the directory for the embedded Crush database and any
// embedded-host-local config persistence.
func WithDataDir(dir string) Option {
	return func(opts *appOptions) {
		opts.dataDir = dir
	}
}

// WithDebug toggles Crush debug mode.
func WithDebug(debug bool) Option {
	return func(opts *appOptions) {
		opts.debug = debug
	}
}

// WithSkipPermissionRequests controls the runtime permission override. It
// defaults to true for embedded hosts.
func WithSkipPermissionRequests(skip bool) Option {
	return func(opts *appOptions) {
		opts.skipPermissionRequests = skip
	}
}

// WithDisableUpdateCheck controls Crush's background update check. It defaults
// to true for embedded hosts to avoid network side effects.
func WithDisableUpdateCheck(disable bool) Option {
	return func(opts *appOptions) {
		opts.disableUpdateCheck = disable
	}
}

// WithSkillsDirs sets the skills paths in the config options.
func WithSkillsDirs(dirs []string) Option {
	return func(opts *appOptions) {
		cfg := opts.ensureConfig()
		if cfg.Options == nil {
			cfg.Options = &config.Options{}
		}
		cfg.Options.SkillsPaths = dirs
	}
}

// WithMCPs sets the MCP servers in the config.
func WithMCPs(mcps MCPs) Option {
	return func(opts *appOptions) {
		opts.ensureConfig().MCP = mcps
	}
}

// WithLSPs sets the LSP servers in the config.
func WithLSPs(lsps LSPs) Option {
	return func(opts *appOptions) {
		opts.ensureConfig().LSP = lsps
	}
}

// WithProviders sets the providers in the config.
func WithProviders(providers *Providers) Option {
	return func(opts *appOptions) {
		opts.ensureConfig().Providers = providers
	}
}

// WithModels sets the selected large/small model map in the config.
func WithModels(models map[SelectedModelType]SelectedModel) Option {
	return func(opts *appOptions) {
		opts.ensureConfig().Models = models
	}
}

// WithAllowedTools sets tools that do not require permission prompts.
func WithAllowedTools(tools []string) Option {
	return func(opts *appOptions) {
		cfg := opts.ensureConfig()
		if cfg.Permissions == nil {
			cfg.Permissions = &config.Permissions{}
		}
		cfg.Permissions.AllowedTools = tools
	}
}

// WithDisabledTools hides built-in tools from the agent.
func WithDisabledTools(tools []string) Option {
	return func(opts *appOptions) {
		cfg := opts.ensureConfig()
		if cfg.Options == nil {
			cfg.Options = &config.Options{}
		}
		cfg.Options.DisabledTools = tools
	}
}

// WithOptions allows callers to edit the embedded Config.Options object.
func WithOptions(update func(*Options)) Option {
	return func(opts *appOptions) {
		cfg := opts.ensureConfig()
		if cfg.Options == nil {
			cfg.Options = &config.Options{}
		}
		if update != nil {
			update(cfg.Options)
		}
	}
}

func (opts *appOptions) ensureConfig() *Config {
	if opts.config == nil {
		opts.config = defaultEmbeddedConfig()
	}
	return opts.config
}

// NewConfig returns a fresh embedded-host-friendly Crush config.
func NewConfig() *Config {
	return defaultEmbeddedConfig()
}

// NewApp creates a new embedded Crush app.
//
// appDir is the project working directory. uuid is an opaque caller-supplied
// app identifier. sessionID is an optional Crush session ID to continue when
// Run is used.
func NewApp(ctx context.Context, appDir, uuid string, sessionID string, opts ...Option) (*App, error) {
	if appDir == "" {
		return nil, fmt.Errorf("appDir is required")
	}
	if abs, err := filepath.Abs(appDir); err == nil {
		appDir = abs
	}

	options := appOptions{
		config:                 defaultEmbeddedConfig(),
		dataDir:                filepath.Join(appDir, "data"),
		skipPermissionRequests: true,
		disableUpdateCheck:     true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.dataDir == "" {
		options.dataDir = filepath.Join(appDir, "data")
	}
	if abs, err := filepath.Abs(options.dataDir); err == nil {
		options.dataDir = abs
	}

	if err := os.MkdirAll(options.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create embedded crush data directory: %w", err)
	}

	store, err := config.LoadEmbedded(options.config, appDir, options.dataDir, options.debug)
	if err != nil {
		return nil, err
	}
	store.Overrides().SkipPermissionRequests = options.skipPermissionRequests
	store.Overrides().DisableUpdateCheck = options.disableUpdateCheck

	conn, err := db.Connect(ctx, options.dataDir)
	if err != nil {
		return nil, err
	}

	internalApp, err := app.New(ctx, conn, store)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	wrappedSessions := newSessionService(internalApp.Sessions, sessionID)
	internalApp.Sessions = wrappedSessions

	return &App{internal: internalApp, id: uuid, sessions: wrappedSessions}, nil
}

func defaultEmbeddedConfig() *Config {
	progress := false
	autoLSP := false
	return &config.Config{
		Schema: defaultSchema,
		Options: &config.Options{
			DisableProviderAutoUpdate: true,
			DisableMetrics:            true,
			DisableNotifications:      true,
			Progress:                  &progress,
			AutoLSP:                   &autoLSP,
		},
		Permissions: &config.Permissions{},
	}
}

type sessionService struct {
	session.Service
	mu        sync.RWMutex
	current   string
	sessionID string
}

func newSessionService(internal session.Service, sessionID string) *sessionService {
	return &sessionService{Service: internal, sessionID: sessionID}
}

func (ss *sessionService) Create(ctx context.Context, title string) (session.Session, error) {
	if ss.sessionID != "" {
		s, err := ss.Service.Get(ctx, ss.sessionID)
		if err == nil {
			ss.setCurrent(s.ID)
			return s, nil
		}
	}

	s, err := ss.Service.Create(ctx, title)
	if err == nil {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) Get(ctx context.Context, id string) (session.Session, error) {
	s, err := ss.Service.Get(ctx, id)
	if err == nil && s.ParentSessionID == "" {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) GetLast(ctx context.Context) (session.Session, error) {
	s, err := ss.Service.GetLast(ctx)
	if err == nil {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) CurrentSessionID() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.current
}

func (ss *sessionService) SetCurrentSessionID(id string) {
	ss.setCurrent(id)
}

func (ss *sessionService) setCurrent(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.current = id
}
