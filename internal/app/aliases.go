package app

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	interactivepkg "github.com/xEsk/shellia/internal/interactive"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	sessionpkg "github.com/xEsk/shellia/internal/session"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

type (
	config              = configpkg.Config
	modelConfig         = configpkg.ModelConfig
	ModelConfig         = configpkg.ModelConfig
	FileConfig          = configpkg.FileConfig
	commandEngineMode   = configpkg.CommandEngineMode
	CommandEngineMode   = configpkg.CommandEngineMode
	confirmationDefault = configpkg.ConfirmationDefault
	ConfirmationDefault = configpkg.ConfirmationDefault

	truncationStrategy     = core.TruncationStrategy
	turnOutcome            = core.TurnOutcome
	repeatReason           = core.RepeatReason
	contextInfo            = core.ContextInfo
	historyEntry           = core.HistoryEntry
	interactiveMode        = core.InteractiveMode
	interactiveCommand     = interactivepkg.Command
	observationMemory      = core.ObservationMemory
	pendingProposal        = core.PendingProposal
	sessionState           = core.SessionState
	turnResult             = core.TurnResult
	workflowAttempt        = core.WorkflowAttempt
	commandPlan            = core.CommandPlan
	commandExecution       = core.CommandExecution
	commandBatchResult     = core.CommandBatchResult
	skippedCommand         = core.SkippedCommand
	manualRenderMode       = executorpkg.ManualRenderMode
	interactivePromptError = executorpkg.InteractivePromptError
	traceLogger            = tracepkg.Logger
	presentation           = uipkg.Presentation

	llmPromptRequest      = llmpkg.PromptRequest
	llmResponse           = llmpkg.Response
	chatCompletionRequest = llmpkg.ChatCompletionRequest
	chatMessage           = llmpkg.ChatMessage
	responseFormat        = llmpkg.ResponseFormat
	llmHTTPStatusError    = llmpkg.HTTPStatusError
	capturedStream        = core.CapturedStream
)

const (
	commandEnginePlain       = configpkg.CommandEnginePlain
	commandEngineInteractive = configpkg.CommandEngineInteractive

	confirmationDefaultNone        = configpkg.ConfirmationDefaultNone
	confirmationDefaultYes         = configpkg.ConfirmationDefaultYes
	confirmationDefaultNo          = configpkg.ConfirmationDefaultNo
	confirmationDefaultEdit        = configpkg.ConfirmationDefaultEdit
	confirmationDefaultInteractive = configpkg.ConfirmationDefaultInteractive
	ConfirmationDefaultNone        = configpkg.ConfirmationDefaultNone
	ConfirmationDefaultYes         = configpkg.ConfirmationDefaultYes
	ConfirmationDefaultNo          = configpkg.ConfirmationDefaultNo
	ConfirmationDefaultEdit        = configpkg.ConfirmationDefaultEdit
	ConfirmationDefaultInteractive = configpkg.ConfirmationDefaultInteractive

	CommandEnginePlain       = configpkg.CommandEnginePlain
	CommandEngineInteractive = configpkg.CommandEngineInteractive

	truncationStart = core.TruncationStart
	truncationEnd   = core.TruncationEnd
	truncationMixed = core.TruncationMixed

	turnOutcomeCompleted          = core.TurnOutcomeCompleted
	turnOutcomeBlocked            = core.TurnOutcomeBlocked
	turnOutcomePlanned            = core.TurnOutcomePlanned
	turnOutcomeDeclined           = core.TurnOutcomeDeclined
	turnOutcomeCancelled          = core.TurnOutcomeCancelled
	turnOutcomeTimeout            = core.TurnOutcomeTimeout
	turnOutcomePlanningLimit      = core.TurnOutcomePlanningLimit
	turnOutcomeStructuralError    = core.TurnOutcomeStructuralError
	turnOutcomeNoProgress         = core.TurnOutcomeNoProgress
	repeatReasonUserRequested     = core.RepeatReasonUserRequested
	repeatReasonRetry             = core.RepeatReasonRetry
	repeatReasonVerifyAfterChange = core.RepeatReasonVerifyAfterChange
	repeatReasonPollChangedState  = core.RepeatReasonPollChangedState
	repeatReasonRequired          = core.RepeatReasonRequired

	interactiveModeAI        = core.InteractiveModeAI
	interactiveModeShell     = core.InteractiveModeShell
	defaultPlanningMaxRounds = 4

	interactiveCommandNone    = interactivepkg.CommandNone
	interactiveCommandUnknown = interactivepkg.CommandUnknown
	interactiveCommandExit    = interactivepkg.CommandExit
	interactiveCommandClear   = interactivepkg.CommandClear
	interactiveCommandContext = interactivepkg.CommandContext
	interactiveCommandRetry   = interactivepkg.CommandRetry
	interactiveCommandNew     = interactivepkg.CommandNew
	interactiveCommandShell   = interactivepkg.CommandShell
	interactiveCommandAI      = interactivepkg.CommandAI
	interactiveCommandMode    = interactivepkg.CommandMode
	interactiveCommandModel   = interactivepkg.CommandModel
	interactiveCommandTheme   = interactivepkg.CommandTheme
	interactiveCommandPlan    = interactivepkg.CommandPlan

	manualRenderInline           = executorpkg.ManualRenderInline
	manualRenderDirect           = executorpkg.ManualRenderDirect
	manualRenderInteractive      = executorpkg.ManualRenderInteractive
	manualRenderShellInteractive = executorpkg.ManualRenderShellInteractive
	visualStylePlain             = configpkg.VisualStylePlain

	colorBlue   = uipkg.ColorBlue
	colorCyan   = uipkg.ColorCyan
	colorYellow = uipkg.ColorYellow
)

var (
	errAborted = core.ErrAborted

	parsePlanInstruction    = interactivepkg.ParsePlanInstruction
	parseInteractiveCommand = interactivepkg.ParseCommand
	parseModelCommandName   = interactivepkg.ParseModelCommandName
	parseThemeCommandName   = interactivepkg.ParseThemeCommandName

	defaultConfig                = configpkg.DefaultConfig
	applyFileConfig              = configpkg.ApplyFileConfig
	applyEnvConfig               = configpkg.ApplyEnvConfig
	applyModelEnvOverrides       = configpkg.ApplyModelEnvOverrides
	loadFileConfig               = configpkg.LoadFileConfig
	settingsPath                 = configpkg.SettingsPath
	initConfigFileTo             = configpkg.InitConfigFileTo
	persistDefaultModel          = configpkg.PersistDefaultModel
	persistVisualStyle           = configpkg.PersistVisualStyle
	updateDefaultModelTOML       = configpkg.UpdateDefaultModelTOML
	normalizeConfirmationDefault = configpkg.NormalizeConfirmationDefault
	normalizeVisualStyle         = configpkg.NormalizeVisualStyle
	visualStyles                 = configpkg.VisualStyles

	uiEnabled                       = uipkg.Enabled
	printErrorTo                    = uipkg.PrintErrorTo
	renderPanel                     = uipkg.RenderPanel
	printSessionBannerTo            = uipkg.PrintSessionBannerTo
	readInteractivePrompt           = uipkg.ReadInteractivePromptWithRenderer
	printWarningTo                  = uipkg.PrintWarningTo
	printSeparator                  = uipkg.PrintSeparator
	printInfoTo                     = uipkg.PrintInfoTo
	printContextTo                  = uipkg.PrintContextTo
	printModeStatusTo               = uipkg.PrintModeStatusTo
	printModelSwitchTo              = uipkg.PrintModelSwitchTo
	printNewSessionSeparatorTo      = uipkg.PrintNewSessionSeparatorTo
	clearScreenTo                   = uipkg.ClearScreenTo
	promptPlanExecution             = uipkg.PromptPlanExecution
	printRawPromptsTo               = uipkg.PrintRawPromptsTo
	printSectionTo                  = uipkg.PrintSectionTo
	promptPlanningLimitContinuation = uipkg.PromptPlanningLimitContinuation
	startThinkingIndicator          = uipkg.StartThinkingIndicator
	newRenderer                     = uipkg.NewRenderer

	getContext = executorpkg.GetContext

	updateSessionState              = sessionpkg.UpdateState
	updateSessionStateFromExecution = sessionpkg.UpdateStateFromExecution
	rememberUnfinishedInstruction   = sessionpkg.RememberUnfinishedInstruction
	resolveInstructionForPlanning   = sessionpkg.ResolveInstructionForPlanning
	isProposalDecline               = sessionpkg.IsProposalDecline

	openSessionTrace      = tracepkg.OpenSession
	setTraceVersion       = tracepkg.SetVersion
	traceSessionStartData = tracepkg.SessionStartData
	withTraceTurnID       = tracepkg.WithTurnID
	setUIVersion          = uipkg.SetVersion

	buildLLMPrompts    = llmpkg.BuildPrompts
	callPlanningPrompt = llmpkg.CallPlanningPrompt
	parseResponse      = llmpkg.ParseResponse
	normalizePlan      = llmpkg.NormalizePlan
	doLLMRequest       = llmpkg.DoRequest
)
