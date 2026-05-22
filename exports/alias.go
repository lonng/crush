package exports

import (
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// Re-exported helpers that embedded hosts commonly need.
var (
	// csync
	NewProviders     = csync.NewMap[string, ProviderConfig]
	NewProvidersFrom = csync.NewMapFrom[string, ProviderConfig]

	// log
	SetupLog = log.Setup

	// message
	Assistant = message.Assistant
	System    = message.System
	Tool      = message.Tool
	User      = message.User

	// skills
	DiscoverSkills       = skills.Discover
	DiscoverSkillsStates = skills.DiscoverWithStates
	ParseSkill           = skills.Parse
	ParseSkillContent    = skills.ParseContent
)

const (
	// catwalk
	ProviderTypeAnthropic    = catwalk.TypeAnthropic
	ProviderTypeAzure        = catwalk.TypeAzure
	ProviderTypeBedrock      = catwalk.TypeBedrock
	ProviderTypeGoogle       = catwalk.TypeGoogle
	ProviderTypeOpenAI       = catwalk.TypeOpenAI
	ProviderTypeOpenAICompat = catwalk.TypeOpenAICompat
	ProviderTypeOpenRouter   = catwalk.TypeOpenRouter
	ProviderTypeVercel       = catwalk.TypeVercel
	ProviderTypeVertexAI     = catwalk.TypeVertexAI

	// config
	MCPHttp                = config.MCPHttp
	MCPSSE                 = config.MCPSSE
	MCPStdio               = config.MCPStdio
	SelectedModelTypeLarge = config.SelectedModelTypeLarge
	SelectedModelTypeSmall = config.SelectedModelTypeSmall

	// message
	FinishReasonCanceled  = message.FinishReasonCanceled
	FinishReasonEndTurn   = message.FinishReasonEndTurn
	FinishReasonError     = message.FinishReasonError
	FinishReasonMaxTokens = message.FinishReasonMaxTokens
	FinishReasonToolUse   = message.FinishReasonToolUse
	FinishReasonUnknown   = message.FinishReasonUnknown
)

type (
	// catwalk
	InferenceProvider = catwalk.InferenceProvider
	Model             = catwalk.Model
	ProviderType      = catwalk.Type

	// config
	Completions       = config.Completions
	Config            = config.Config
	LSPConfig         = config.LSPConfig
	LSPs              = config.LSPs
	MCPConfig         = config.MCPConfig
	MCPs              = config.MCPs
	MCPType           = config.MCPType
	Options           = config.Options
	Permissions       = config.Permissions
	ProviderConfig    = config.ProviderConfig
	SelectedModel     = config.SelectedModel
	SelectedModelType = config.SelectedModelType
	TUIOptions        = config.TUIOptions

	// csync
	Providers = csync.Map[string, ProviderConfig]

	// message
	ContentPart           = message.ContentPart
	ContentPartBinary     = message.BinaryContent
	ContentPartFinish     = message.Finish
	ContentPartImageURL   = message.ImageURLContent
	ContentPartReasoning  = message.ReasoningContent
	ContentPartText       = message.TextContent
	ContentPartToolCall   = message.ToolCall
	ContentPartToolResult = message.ToolResult
	CreateMessageParams   = message.CreateMessageParams
	FinishReason          = message.FinishReason
	Message               = message.Message
	MessageRole           = message.MessageRole
	MessageService        = message.Service

	// session
	Session        = session.Session
	SessionService = session.Service
	Todo           = session.Todo
	TodoStatus     = session.TodoStatus

	// skills
	Skill      = skills.Skill
	SkillState = skills.SkillState
)
