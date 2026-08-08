package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

const (
	maxHistoryEntries = 8
	maxCommandTimeout = 24 * time.Hour
	maxRequestTimeout = 10 * time.Minute
	maxCaptureBytes   = 512 * 1024 * 1024 // 512 MB
	maxOutputChars    = 100_000
	maxPlanningRounds = 100
)

var version = "dev"

var errHelp = errors.New("help requested")

var errStructuralResponse = errors.New("invalid structured model response")

// planningRoundRequest groups one model planning attempt and its rendering dependencies.
type planningRoundRequest struct {
	Deps                  runtimeDeps
	UI                    bool
	TurnID                string
	Round                 int
	Prompt                llmPromptRequest
	AllowStructuralRepair bool
}

// planningRoundResult is the normalized output of one planning attempt.
type planningRoundResult struct {
	Parsed               llmResponse
	Summary              string
	Plans                []commandPlan
	StructuralRepairUsed bool
}

// turnRequest groups the context needed to process one user instruction.
type turnRequest struct {
	Config              config
	ContextInfo         *contextInfo
	Instruction         string
	ResolvedInstruction string
	AcceptedProposal    bool
	History             []historyEntry
	State               sessionState
}

// SetVersion records the binary version for UI and trace metadata.
func SetVersion(value string) {
	version = strings.TrimSpace(value)
	if version == "" {
		version = "dev"
	}
	setTraceVersion(version)
	setUIVersion(version)
}

// Run executes the Shellia CLI and returns the process exit code.
func Run(parentCtx context.Context, args []string) int {
	return runApp(parentCtx, args, defaultRuntimeDeps())
}

func runApp(parentCtx context.Context, args []string, deps runtimeDeps) int {
	deps = deps.withDefaults()

	cfg, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, errHelp) {
			return 0
		}
		printErrorTo(deps.Stderr, uiEnabled(config{}), err.Error())
		return 2
	}

	effective := effectivePresentation(cfg, deps)
	ui := effective.ANSI

	switch cfg.CommandKind {
	case "config-init":
		if err := initConfigFileTo(deps.Stdout, ui); err != nil {
			printErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
		return 0
	case "config-path":
		path, err := settingsPath()
		if err != nil {
			printErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
		renderPanel(deps.Stdout, ui, "config", colorCyan, []string{path})
		return 0
	}

	appCtx := parentCtx
	stop := func() {}
	if !cfg.Interactive {
		// One-shot runs exit when Ctrl+C cancels the application context.
		appCtx, stop = signal.NotifyContext(parentCtx, os.Interrupt)
	}
	defer stop()

	ctxInfo, err := getContext(appCtx, cfg)
	if err != nil {
		printErrorTo(deps.Stderr, ui, err.Error())
		return 1
	}
	deps.Renderer = newRenderer(deps.Stdout, presentation{Style: effective.Style, ANSI: effective.ANSI, User: promptPresentationUser(cfg, ctxInfo.User)})

	trace, err := openSessionTrace(cfg, ctxInfo)
	if err != nil {
		printErrorTo(deps.Stderr, ui, err.Error())
		return 1
	}
	deps.Trace = trace
	if trace != nil {
		trace.Record("session_start", "", "", -1, traceSessionStartData(cfg, ctxInfo))
		defer func() {
			trace.Record("session_end", "", "", -1, nil)
			_ = trace.Close()
		}()
	}

	if cfg.Interactive {
		runInteractive(appCtx, deps, ui, cfg, &ctxInfo)
		return 0
	}

	turn, err := runTurn(appCtx, deps, ui, turnRequest{
		Config:      cfg,
		ContextInfo: &ctxInfo,
		Instruction: cfg.Instruction,
	})
	if err != nil {
		switch {
		case errors.Is(err, errAborted), errors.Is(err, context.Canceled):
			printErrorTo(deps.Stderr, ui, "execution aborted")
			return 130
		default:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code := exitErr.ExitCode()
				if code <= 0 {
					code = 1
				}
				printErrorTo(deps.Stderr, ui, err.Error())
				return code
			}
			printErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
	}
	return oneShotExitCode(turn.Outcome)
}

func oneShotExitCode(outcome turnOutcome) int {
	switch outcome {
	case turnOutcomeCompleted, turnOutcomePlanned, turnOutcomeDeclined:
		return 0
	default:
		return 1
	}
}

// parseArgs processes CLI config and validates the minimum required values.
func parseArgs(args []string) (config, error) {
	if kind, ok, err := parseConfigSubcommand(args); ok || err != nil {
		if err != nil {
			return config{}, err
		}
		cfg := defaultConfig()
		applyEnvConfig(&cfg)
		cfg.CommandKind = kind
		return cfg, nil
	}

	cfg, err := loadBaseConfig()
	if err != nil {
		return config{}, err
	}

	fs, timeoutSecs, reqTimeoutSecs := buildFlagSet(&cfg)

	if isHelpRequest(args) {
		fs.Usage()
		return config{}, errHelp
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return config{}, errHelp
		}
		return config{}, fmt.Errorf("invalid arguments: %w", err)
	}

	return finalizeConfig(fs, cfg, *timeoutSecs, *reqTimeoutSecs)
}

// loadBaseConfig applies defaults → file → env in order.
func loadBaseConfig() (config, error) {
	cfg := defaultConfig()
	fileCfg, configPath, err := loadFileConfig()
	if err != nil {
		return config{}, err
	}
	cfg.ConfigPath = configPath
	applyFileConfig(&cfg, fileCfg)
	applyEnvConfig(&cfg)
	return cfg, nil
}

// parseConfigSubcommand detects `shellia config init|path` and returns the command kind.
func parseConfigSubcommand(args []string) (string, bool, error) {
	if len(args) == 0 || args[0] != "config" {
		return "", false, nil
	}
	if len(args) != 2 {
		return "", true, fmt.Errorf("invalid config command: use shellia config init|path")
	}
	switch args[1] {
	case "init":
		return "config-init", true, nil
	case "path":
		return "config-path", true, nil
	}
	return "", true, fmt.Errorf("invalid config command %q: use shellia config init|path", args[1])
}

// buildFlagSet registers all CLI flags and returns the set plus timeout int pointers.
func buildFlagSet(cfg *config) (*flag.FlagSet, *int, *int) {
	fs := flag.NewFlagSet("shellia", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	timeoutSecs := int(cfg.CommandTimeout.Seconds())
	reqTimeoutSecs := int(cfg.RequestTimeout.Seconds())

	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "base URL of the OpenAI-compatible API")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "API key")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "model to use")
	fs.StringVar(&cfg.ModelName, "model-name", cfg.ModelName, "configured model profile to use")
	fs.IntVar(&timeoutSecs, "timeout", timeoutSecs, "per-command timeout in seconds")
	fs.IntVar(&reqTimeoutSecs, "request-timeout", reqTimeoutSecs, "HTTP request timeout in seconds")
	fs.BoolVar(&cfg.YesSafe, "yes-safe", cfg.YesSafe, "auto-execute safe commands without confirmation")
	fs.BoolVar(&cfg.ContinueOnError, "continue-on-error", cfg.ContinueOnError, "continue if a command fails")
	fs.BoolVar(&cfg.AskConfirmPlan, "ask-confirm-plan", cfg.AskConfirmPlan, "ask for confirmation before executing the plan")
	fs.BoolVar(&cfg.Interactive, "interactive", false, "start or maintain an interactive session")
	fs.BoolVar(&cfg.Interactive, "i", false, "short alias for --interactive")
	fs.BoolVar(&cfg.PlanOnly, "plan", cfg.PlanOnly, "show the command plan without executing it")
	fs.BoolVar(&cfg.PlanOnly, "p", false, "short alias for --plan")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "show context and debug data")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "show full plan and technical detail")
	fs.BoolVar(&cfg.RawPrompt, "raw-prompt", cfg.RawPrompt, "print the raw model prompts")
	fs.BoolVar(&cfg.RawResponse, "raw-response", cfg.RawResponse, "print the raw model response")
	fs.BoolVar(&cfg.TraceEnabled, "trace", cfg.TraceEnabled, "write a JSONL diagnostic trace for this session")
	fs.StringVar(&cfg.TraceDir, "trace-dir", cfg.TraceDir, "directory for JSONL diagnostic trace files")
	fs.BoolVar(&cfg.NoColor, "no-color", cfg.NoColor, "disable UI colours")
	fs.Usage = usageFunc(fs)

	return fs, &timeoutSecs, &reqTimeoutSecs
}

// isHelpRequest returns true for the explicit -h/--help shortcut before flag parsing.
func isHelpRequest(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch strings.TrimSpace(args[0]) {
	case "-h", "-help", "--help":
		return true
	}
	return false
}

// finalizeConfig applies the remaining positional args and validates the result.
func finalizeConfig(fs *flag.FlagSet, cfg config, timeoutSecs, reqTimeoutSecs int) (config, error) {
	flagBaseURL := cfg.BaseURL
	flagAPIKey := cfg.APIKey
	flagModel := cfg.Model
	flagBaseURLSet := flagWasSet(fs, "base-url")
	flagAPIKeySet := flagWasSet(fs, "api-key")
	flagModelSet := flagWasSet(fs, "model")

	if timeoutSecs <= 0 {
		return config{}, fmt.Errorf("timeout must be greater than 0")
	}
	if reqTimeoutSecs <= 0 {
		return config{}, fmt.Errorf("request-timeout must be greater than 0")
	}
	if timeoutSecs > int(maxCommandTimeout/time.Second) {
		return config{}, fmt.Errorf("timeout_seconds too large (max %v)", maxCommandTimeout)
	}
	if reqTimeoutSecs > int(maxRequestTimeout/time.Second) {
		return config{}, fmt.Errorf("request_timeout_seconds too large (max %v)", maxRequestTimeout)
	}

	cfg.CommandTimeout = time.Duration(timeoutSecs) * time.Second
	cfg.RequestTimeout = time.Duration(reqTimeoutSecs) * time.Second

	if cfg.CaptureStdoutBytes > maxCaptureBytes || cfg.CaptureStderrBytes > maxCaptureBytes {
		return config{}, fmt.Errorf("capture byte limits cannot exceed %d bytes", maxCaptureBytes)
	}
	if cfg.ObservationOutputChars > maxOutputChars {
		return config{}, fmt.Errorf("output char limits cannot exceed %d", maxOutputChars)
	}
	if cfg.PlanningMaxRounds <= 0 {
		return config{}, fmt.Errorf("planning_max_rounds must be greater than 0")
	}
	if cfg.PlanningMaxRounds > maxPlanningRounds {
		return config{}, fmt.Errorf("planning_max_rounds cannot exceed %d", maxPlanningRounds)
	}
	if err := applySelectedModel(&cfg); err != nil {
		return config{}, err
	}
	applyModelEnvOverrides(&cfg)
	if flagBaseURLSet {
		cfg.BaseURL = strings.TrimSpace(flagBaseURL)
	}
	if flagAPIKeySet {
		cfg.APIKey = strings.TrimSpace(flagAPIKey)
	}
	if flagModelSet {
		cfg.Model = strings.TrimSpace(flagModel)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return config{}, fmt.Errorf("missing model configuration: configure [[models]] or pass --base-url and --model")
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		cfg.Interactive = true
		if requiresAPIKey(cfg) {
			return config{}, missingAPIKeyError()
		}
		return cfg, nil
	}

	cfg.Instruction = strings.Join(remaining, " ")

	if cfg.CommandKind != "" {
		return cfg, nil
	}

	if requiresAPIKey(cfg) {
		return config{}, missingAPIKeyError()
	}

	return cfg, nil
}

// flagWasSet reports whether a CLI flag was explicitly provided.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	wasSet := false
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

// applySelectedModel resolves the active configured model profile.
func applySelectedModel(cfg *config) error {
	if len(cfg.Models) == 0 {
		if strings.TrimSpace(cfg.ModelName) != "" {
			return fmt.Errorf("configured model profile %q not found", strings.TrimSpace(cfg.ModelName))
		}
		cfg.SupportsResponseFormat = true
		return nil
	}

	if err := validateModelConfigs(cfg.Models); err != nil {
		return err
	}

	name := strings.TrimSpace(cfg.ModelName)
	if name == "" {
		name = strings.TrimSpace(cfg.DefaultModelName)
	}
	if name == "" {
		name = cfg.Models[0].Name
	}

	selected, ok := findModelConfig(cfg.Models, name)
	if !ok {
		return fmt.Errorf("configured model profile %q not found", name)
	}

	applyModelConfig(cfg, selected)
	return nil
}

// applyModelConfig applies one configured model profile to the runtime config.
func applyModelConfig(cfg *config, selected modelConfig) {
	cfg.ModelName = selected.Name
	cfg.BaseURL = selected.BaseURL
	cfg.Model = selected.Model
	cfg.APIKey = selected.APIKey
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(selected.APIKeyEnv) != "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv(selected.APIKeyEnv))
	}
	cfg.SupportsResponseFormat = selected.SupportsResponseFormat
}

// validateModelConfigs checks configured model profiles before selection.
func validateModelConfigs(models []modelConfig) error {
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.Name) == "" {
			return fmt.Errorf("configured model profile is missing name")
		}
		if seen[model.Name] {
			return fmt.Errorf("configured model profile %q is duplicated", model.Name)
		}
		seen[model.Name] = true
		if strings.TrimSpace(model.BaseURL) == "" {
			return fmt.Errorf("configured model profile %q is missing base_url", model.Name)
		}
		if strings.TrimSpace(model.Model) == "" {
			return fmt.Errorf("configured model profile %q is missing model", model.Name)
		}
	}
	return nil
}

// findModelConfig finds a configured model profile by name.
func findModelConfig(models []modelConfig, name string) (modelConfig, bool) {
	for _, model := range models {
		if model.Name == name {
			return model, true
		}
	}
	return modelConfig{}, false
}

// requiresAPIKey reports whether the configured endpoint needs an explicit API key.
func requiresAPIKey(cfg config) bool {
	return strings.TrimSpace(cfg.APIKey) == "" && !allowsEmptyAPIKey(cfg)
}

// allowsEmptyAPIKey permits local OpenAI-compatible servers such as MLX Server.
func allowsEmptyAPIKey(cfg config) bool {
	parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// missingAPIKeyError returns the shared user-facing API key validation error.
func missingAPIKeyError() error {
	return fmt.Errorf("missing api key: use --api-key or set SHELLIA_API_KEY")
}

// usageFunc builds the Usage closure for the flag set.
func usageFunc(fs *flag.FlagSet) func() {
	return func() {
		configPath, _ := settingsPath()
		fmt.Fprintf(os.Stdout, "shellia %s\n\n", version)
		fmt.Fprintln(os.Stdout, "Usage:")
		fmt.Fprintln(os.Stdout, "  shellia")
		fmt.Fprintln(os.Stdout, `  shellia [flags] "your instruction here"`)
		fmt.Fprintln(os.Stdout, "  shellia config init")
		fmt.Fprintln(os.Stdout, "  shellia config path")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Flags:")
		fs.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(os.Stdout, "  -%s: %s\n", f.Name, f.Usage)
		})
		if strings.TrimSpace(configPath) != "" {
			fmt.Fprintln(os.Stdout)
			fmt.Fprintf(os.Stdout, "Config:\n  %s\n", configPath)
		}
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "Examples:")
		fmt.Fprintln(os.Stdout, "  shellia")
		fmt.Fprintln(os.Stdout, `  shellia --api-key "YOUR_KEY" "run git status"`)
		fmt.Fprintln(os.Stdout, `  shellia -i "run git status"`)
		fmt.Fprintln(os.Stdout, "  shellia config init")
	}
}

// --- Session flow ---

// runInteractive opens a persistent session where each prompt extends the conversation context.
// A fresh signal context is created per turn so Ctrl+C cancels only the current LLM call,
// allowing the loop to continue for the next request.
func runInteractive(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo) {
	deps = deps.withDefaults()
	if deps.Renderer == nil {
		deps.Renderer = newRenderer(deps.Stdout, presentation{Style: visualStylePlain, ANSI: ui, User: promptPresentationUser(cfg, ctxInfo.User)})
	}
	reader := bufio.NewReader(deps.Stdin)
	history := make([]historyEntry, 0, maxHistoryEntries)
	state := sessionState{}
	mode := interactiveModeAI

	printSessionBannerTo(deps.Stdout, ui, cfg)

	if strings.TrimSpace(cfg.Instruction) != "" {
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, deps, ui, turnRequest{
			Config:      cfg,
			ContextInfo: ctxInfo,
			Instruction: cfg.Instruction,
			History:     history,
			State:       state,
		})
		stop()
		if err != nil && len(turn.Executions) > 0 {
			updateSessionState(&state, cfg.Instruction, turn, cfg)
		}
		if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
			state.LastRetryInstruction = cfg.Instruction
			rememberUnfinishedInstruction(&state, cfg.Instruction)
		} else if err != nil {
			printWarningTo(deps.Stderr, ui, err.Error())
			state.LastRetryInstruction = cfg.Instruction
			rememberUnfinishedInstruction(&state, cfg.Instruction)
		} else {
			history = append(history, historyEntry{Instruction: cfg.Instruction, Result: turn.Result})
			updateSessionState(&state, cfg.Instruction, turn, cfg)
			if turn.Outcome == turnOutcomeCompleted {
				state.LastRetryInstruction = ""
			}
		}
	}

	for {
		// Check if the parent context was cancelled (e.g. second Ctrl+C).
		if ctx.Err() != nil {
			return
		}

		input, err := readInteractivePrompt(ui, reader, deps.Stdin, deps.Stdout, mode, cfg, deps.Renderer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(deps.Stdout)
				return
			}
			exitWithError(ui, fmt.Sprintf("cannot read prompt: %v", err), 1)
		}

		trimmed := strings.TrimSpace(input)
		forcePromptMode := false
		planOnly, plannedInstruction := parsePlanInstruction(input)
		if planOnly {
			if strings.TrimSpace(plannedInstruction) == "" {
				printWarningTo(deps.Stderr, ui, "Missing plan instruction.")
				continue
			}

			turnCfg := cfg
			turnCfg.PlanOnly = true
			turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			turn, err := runTurn(turnCtx, deps, ui, turnRequest{
				Config:      turnCfg,
				ContextInfo: ctxInfo,
				Instruction: plannedInstruction,
				History:     history,
				State:       state,
			})
			stop()
			if err != nil && len(turn.Executions) > 0 {
				updateSessionState(&state, plannedInstruction, turn, cfg)
			}

			if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
				state.LastRetryInstruction = plannedInstruction
				rememberUnfinishedInstruction(&state, plannedInstruction)
				printWarningTo(deps.Stderr, ui, "Request cancelled.")
				fmt.Fprintln(deps.Stdout)
				printSeparator(deps.Stdout, ui)
				continue
			}
			if err != nil {
				printWarningTo(deps.Stderr, ui, err.Error())
				state.LastRetryInstruction = plannedInstruction
				rememberUnfinishedInstruction(&state, plannedInstruction)
				continue
			}
			history = append(history, historyEntry{Instruction: input, Result: turn.Result})
			updateSessionState(&state, plannedInstruction, turn, cfg)
			if len(history) > maxHistoryEntries {
				history = history[len(history)-maxHistoryEntries:]
			}
			continue
		}

		command := parseInteractiveCommand(trimmed)
		if command != interactiveCommandNone {
			switch command {
			case interactiveCommandUnknown:
				printWarningTo(deps.Stderr, ui, "Unknown command: "+trimmed)
				continue
			case interactiveCommandExit:
				fmt.Fprintln(deps.Stdout)
				printInfoTo(deps.Stdout, ui, "Session closed.")
				return
			case interactiveCommandClear:
				clearScreenTo(deps.Stdout)
				continue
			case interactiveCommandContext:
				printContextTo(deps.Stdout, ui, cfg, *ctxInfo)
				continue
			case interactiveCommandShell:
				mode = interactiveModeShell
				printModeStatusTo(deps.Stdout, ui, fmt.Sprintf("Shell mode enabled (%s).", cfg.ShellMode))
				continue
			case interactiveCommandAI:
				mode = interactiveModeAI
				printModeStatusTo(deps.Stdout, ui, "Prompt mode enabled.")
				continue
			case interactiveCommandMode:
				printModeStatusTo(deps.Stdout, ui, "Current mode: "+string(mode))
				continue
			case interactiveCommandModel:
				modelName := parseModelCommandName(trimmed)
				if modelName == "" {
					printModelProfilesTo(deps.Stdout, ui, cfg)
					continue
				}
				if err := switchInteractiveModel(&cfg, modelName); err != nil {
					printWarningTo(deps.Stderr, ui, err.Error())
					continue
				}
				printModelSwitchTo(deps.Stdout, ui, cfg)
				continue
			case interactiveCommandTheme:
				themeName := parseThemeCommandName(trimmed)
				if themeName == "" {
					printVisualThemesTo(deps.Stdout, ui, cfg)
					continue
				}
				if err := switchInteractiveTheme(&cfg, &deps, ctxInfo.User, themeName); err != nil {
					printWarningTo(deps.Stderr, ui, err.Error())
					continue
				}
				printInfoTo(deps.Stdout, ui, "Theme switched to "+string(cfg.VisualStyle)+".")
				continue
			case interactiveCommandPlan:
				printWarningTo(deps.Stderr, ui, "Missing plan instruction.")
				continue
			case interactiveCommandRetry:
				if strings.TrimSpace(state.LastRetryInstruction) == "" {
					printWarningTo(deps.Stderr, ui, "No failed or cancelled request to retry.")
					continue
				}
				trimmed = state.LastRetryInstruction
				input = state.LastRetryInstruction
				forcePromptMode = true
				printInfoTo(deps.Stdout, ui, fmt.Sprintf("Retrying: %s", input))
			case interactiveCommandNew:
				history = make([]historyEntry, 0, maxHistoryEntries)
				state = sessionState{}
				printNewSessionSeparatorTo(deps.Stdout, ui)
				continue
			}
		}

		if trimmed == "" {
			continue
		}

		if !forcePromptMode && mode == interactiveModeAI && strings.TrimSpace(state.PendingProposal.Objective) != "" && isProposalDecline(trimmed) {
			declined := state.PendingProposal
			state.PendingProposal = pendingProposal{}
			deps.Trace.Record("pending_proposal_declined", "", "session", -1, map[string]any{
				"objective": declined.Objective,
			})
			printInfoTo(deps.Stdout, ui, "D’acord. No ho executaré.")
			continue
		}

		if !forcePromptMode && (mode == interactiveModeShell || strings.HasPrefix(trimmed, "!")) {
			command := trimmed
			renderMode := renderModeForShellSession(cfg)
			if mode != interactiveModeShell {
				command = strings.TrimSpace(strings.TrimPrefix(command, "!"))
				renderMode = renderModeForManualCommand(cfg)
			}
			if command == "" {
				printWarningTo(deps.Stderr, ui, "Missing shell command.")
				continue
			}

			turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			state.LastObservationObjective = ""
			execution, err := deps.ExecuteManualCommand(turnCtx, deps, ui, cfg, ctxInfo, command, renderMode)
			stop()

			if errors.Is(err, context.Canceled) {
				printWarningTo(deps.Stderr, ui, "Command cancelled.")
				continue
			}
			if err != nil {
				printWarningTo(deps.Stderr, ui, err.Error())
				continue
			}

			updateSessionStateFromExecution(&state, command, execution, cfg)
			continue
		}

		instruction := input
		priorProposal := state.PendingProposal
		resolvedInstruction := resolveInstructionForPlanning(instruction, state)
		acceptedProposal := resolvedInstruction != strings.TrimSpace(instruction) && strings.TrimSpace(state.PendingProposal.Objective) != ""
		retryInstruction := instruction
		if acceptedProposal {
			retryInstruction = resolvedInstruction
			state.PendingProposal = pendingProposal{}
		}

		// Per-turn signal context: Ctrl+C cancels only this turn, not the whole session.
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, deps, ui, turnRequest{
			Config:              cfg,
			ContextInfo:         ctxInfo,
			Instruction:         instruction,
			ResolvedInstruction: resolvedInstruction,
			AcceptedProposal:    acceptedProposal,
			History:             history,
			State:               state,
		})
		stop()
		if !acceptedProposal && strings.TrimSpace(priorProposal.Objective) != "" {
			state.PendingProposal = pendingProposal{}
		}
		if err != nil && len(turn.Executions) > 0 {
			updateSessionState(&state, retryInstruction, turn, cfg)
		}

		if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
			state.LastRetryInstruction = retryInstruction
			rememberUnfinishedInstruction(&state, retryInstruction)
			printWarningTo(deps.Stderr, ui, "Request cancelled.")
			fmt.Fprintln(deps.Stdout)
			printSeparator(deps.Stdout, ui)
			continue
		}
		if err != nil {
			printWarningTo(deps.Stderr, ui, err.Error())
			state.LastRetryInstruction = retryInstruction
			rememberUnfinishedInstruction(&state, retryInstruction)
			continue
		}
		if !acceptedProposal && strings.TrimSpace(priorProposal.Objective) != "" && turn.Outcome == turnOutcomeCompleted && strings.TrimSpace(turn.Proposal.Objective) != strings.TrimSpace(priorProposal.Objective) {
			deps.Trace.Record("pending_proposal_replaced", "", "session", -1, map[string]any{
				"previous_objective":    priorProposal.Objective,
				"replacement_objective": turn.Proposal.Objective,
			})
		}
		history = append(history, historyEntry{Instruction: instruction, Result: turn.Result})
		updateSessionState(&state, retryInstruction, turn, cfg)
		if acceptedProposal && turn.Outcome != turnOutcomeCompleted && turn.Outcome != turnOutcomeDeclined {
			state.LastRetryInstruction = retryInstruction
			rememberUnfinishedInstruction(&state, retryInstruction)
		}
		if turn.Outcome == turnOutcomeCompleted {
			state.LastRetryInstruction = ""
		}
		if len(history) > maxHistoryEntries {
			history = history[len(history)-maxHistoryEntries:]
		}
	}
}

// renderModeForShellSession maps the configured shell mode to the executor mode.
func renderModeForShellSession(cfg config) manualRenderMode {
	if cfg.ShellMode == commandEnginePlain {
		return manualRenderDirect
	}
	return manualRenderShellInteractive
}

// renderModeForManualCommand maps the configured one-off command mode to the executor mode.
func renderModeForManualCommand(cfg config) manualRenderMode {
	if cfg.CommandMode == commandEngineInteractive {
		return manualRenderInteractive
	}
	return manualRenderInline
}

// printModelProfilesTo lists configured model profiles and marks the active one.
func printModelProfilesTo(target io.Writer, ui bool, cfg config) {
	if len(cfg.Models) == 0 {
		renderPanel(target, ui, "models", colorYellow, []string{
			"No configured model profiles.",
			"Add [[models]] entries to config.toml.",
		})
		return
	}

	lines := make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		marker := " "
		if model.Name == cfg.ModelName {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s", marker, model.Name, model.Model))
	}
	renderPanel(target, ui, "models", colorCyan, lines)
}

// printVisualThemesTo lists every selectable visual style and marks the configured one.
func printVisualThemesTo(target io.Writer, ui bool, cfg config) {
	lines := make([]string, 0, len(visualStyles()))
	for _, visualStyle := range visualStyles() {
		marker := " "
		if visualStyle == cfg.VisualStyle {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, visualStyle))
	}
	renderPanel(target, ui, "themes", colorCyan, lines)
}

// switchInteractiveModel applies a configured model profile and persists it as the default.
func switchInteractiveModel(cfg *config, name string) error {
	if len(cfg.Models) == 0 {
		return fmt.Errorf("no configured model profiles")
	}
	if err := validateModelConfigs(cfg.Models); err != nil {
		return err
	}

	selected, ok := findModelConfig(cfg.Models, strings.TrimSpace(name))
	if !ok {
		return fmt.Errorf("configured model profile %q not found", strings.TrimSpace(name))
	}

	next := *cfg
	applyModelConfig(&next, selected)
	if requiresAPIKey(next) {
		return missingAPIKeyError()
	}

	*cfg = next
	cfg.DefaultModelName = selected.Name
	if err := persistDefaultModel(*cfg, selected.Name); err != nil {
		return fmt.Errorf("model switched to %s, but default_model was not persisted: %w", selected.Name, err)
	}
	return nil
}

// switchInteractiveTheme persists and applies one visual style between turns.
func switchInteractiveTheme(cfg *config, deps *runtimeDeps, user string, name string) error {
	selected := normalizeVisualStyle(name, "")
	if selected == "" {
		return fmt.Errorf("visual theme %q not found", strings.TrimSpace(name))
	}

	next := *cfg
	next.VisualStyle = selected
	if err := persistVisualStyle(next, selected); err != nil {
		return fmt.Errorf("theme was not changed: %w", err)
	}

	effective := effectivePresentation(next, *deps)
	nextRenderer := newRenderer(deps.Stdout, presentation{
		Style: effective.Style,
		ANSI:  effective.ANSI,
		User:  promptPresentationUser(next, user),
	})
	*cfg = next
	deps.Renderer = nextRenderer
	return nil
}

// runTurn executes a full plan → confirm → execute → answer cycle, or stops after planning in plan-only mode.
func runTurn(ctx context.Context, deps runtimeDeps, ui bool, request turnRequest) (result turnResult, err error) {
	deps = deps.withDefaults()
	cfg := request.Config
	ctxInfo := request.ContextInfo
	instruction := request.Instruction
	history := request.History
	state := request.State
	objective := strings.TrimSpace(request.ResolvedInstruction)
	if objective == "" {
		objective = instruction
	}
	workflow := newWorkflowState(objective, cfg.PlanOnly, cfg.PlanningMaxRounds)
	workflow.priorEvidenceAvailable = len(state.LastObservations) > 0 &&
		strings.EqualFold(strings.TrimSpace(state.LastRetryInstruction), strings.TrimSpace(objective)) &&
		strings.EqualFold(strings.TrimSpace(state.LastObservationObjective), strings.TrimSpace(objective))
	turnID := deps.Trace.StartTurn(map[string]any{
		"instruction":        instruction,
		"resolved_objective": objective,
		"execution_allowed":  workflow.canExecute(),
		"cwd":                ctxInfo.CWD,
		"history_count":      len(history),
		"state":              state,
	})
	ctx = withTraceTurnID(ctx, turnID)
	if request.AcceptedProposal {
		deps.Trace.Record("pending_proposal_accepted", turnID, "session", -1, map[string]any{
			"objective": objective,
		})
	}
	defer func() {
		if err != nil && result.Outcome == "" {
			switch {
			case errors.Is(err, context.Canceled):
				result.Outcome = turnOutcomeCancelled
				result.BlockerKind = "cancelled"
				result.BlockerReason = "The turn was cancelled."
			case errors.Is(err, context.DeadlineExceeded):
				result.Outcome = turnOutcomeTimeout
				result.BlockerKind = "timeout"
				result.BlockerReason = "The turn timed out."
			default:
				result.Outcome = turnOutcomeStructuralError
				result.BlockerKind = "structural_error"
				result.BlockerReason = err.Error()
			}
		}
		data := map[string]any{
			"result":           result.Result,
			"outcome":          result.Outcome,
			"blocker_kind":     result.BlockerKind,
			"blocker_reason":   result.BlockerReason,
			"plans_count":      len(result.Plans),
			"executions_count": len(result.Executions),
			"skipped_count":    len(result.Skipped),
		}
		if err != nil {
			data["error"] = err.Error()
		}
		deps.Trace.Record("turn_end", turnID, "", -1, data)
	}()

	if cfg.Debug || cfg.Verbose {
		printContextTo(deps.Stdout, ui, cfg, *ctxInfo)
	}

	if deps.Renderer == nil {
		deps.Renderer = newRenderer(deps.Stdout, presentation{Style: visualStylePlain, ANSI: ui, User: promptPresentationUser(cfg, ctxInfo.User)})
	}
	turnUI := deps.Renderer.BeginShelliaTurn(cfg, *ctxInfo)
	defer turnUI.Close()
	deps.Turn = turnUI
	partialResult := func() turnResult {
		return workflow.result("", "", "")
	}

	for workflow.round = 0; ; workflow.round++ {
		round := workflow.round
		var previousDecision *llmResponse
		if workflow.lastDecision.Action != "" {
			previousDecision = &workflow.lastDecision
		}
		promptRequest := llmPromptRequest{
			Config:                    cfg,
			ContextInfo:               *ctxInfo,
			Instruction:               workflow.objective,
			History:                   history,
			State:                     state,
			Observations:              workflow.executions,
			Skipped:                   workflow.skipped,
			LatestBatchExecutionStart: workflow.latestBatchExecutionStart,
			LatestBatchSkippedStart:   workflow.latestBatchSkippedStart,
			EvidenceRevision:          workflow.evidenceRevision,
			PlanningRoundsRemaining:   workflow.planningBudget - round,
			ObjectiveMode:             workflow.objectiveMode,
			SuccessCriteria:           workflow.successCriteria,
			DecisionError:             workflow.decisionError,
			PriorEvidenceAvailable:    workflow.priorEvidenceAvailable,
			PreviousDecision:          previousDecision,
			Attempts:                  workflow.attempts,
		}
		roundResult, err := runPlanningRound(ctx, planningRoundRequest{
			Deps:                  deps,
			UI:                    ui,
			TurnID:                turnID,
			Round:                 round,
			Prompt:                promptRequest,
			AllowStructuralRepair: !workflow.structuralRepairUsed,
		})
		if err != nil {
			failed := partialResult()
			switch {
			case errors.Is(err, context.Canceled):
				failed.Outcome = turnOutcomeCancelled
				failed.BlockerKind = "cancelled"
				failed.BlockerReason = "The planning request was cancelled."
			case errors.Is(err, context.DeadlineExceeded):
				failed.Outcome = turnOutcomeTimeout
				failed.BlockerKind = "timeout"
				failed.BlockerReason = "The planning request timed out."
			case errors.Is(err, errStructuralResponse):
				failed.Outcome = turnOutcomeStructuralError
				failed.BlockerKind = "structural_error"
				failed.BlockerReason = err.Error()
			default:
				failed.Outcome = turnOutcomeBlocked
				failed.BlockerKind = "unavailable"
				failed.BlockerReason = err.Error()
			}
			return failed, err
		}
		if roundResult.StructuralRepairUsed {
			workflow.structuralRepairUsed = true
		}
		parsed := roundResult.Parsed
		summary := roundResult.Summary
		plans := roundResult.Plans

		if decisionErr := workflow.validateDecision(parsed); decisionErr != nil {
			workflow.recordDecision(parsed, summary, plans)
			workflow.decisionError = decisionErr.Error()
			deps.Trace.Record("completion_validation", turnID, "planning", round, map[string]any{
				"objective_mode":   parsed.ObjectiveMode,
				"success_criteria": parsed.SuccessCriteria,
				"basis_type":       parsed.CompletionBasis.Type,
				"admitted":         false,
				"reason":           decisionErr.Error(),
			})
			if workflow.semanticRepairUsed {
				failed := workflow.result(turnOutcomeStructuralError, "structural_error", decisionErr.Error())
				validationFailure := "Shellia could not validate the model's final response. The observed command output remains available above; retry the request if needed."
				if len(workflow.executions) == 0 {
					validationFailure = "Shellia could not validate the model's final response. No commands were executed; retry the request if needed."
				}
				failed.Result = validationFailure
				turnUI.Final(validationFailure)
				return failed, nil
			}
			workflow.semanticRepairUsed = true
			continue
		}
		workflow.decisionError = ""
		workflow.recordDecision(parsed, summary, plans)
		workflow.proposal = pendingProposal{Objective: strings.TrimSpace(parsed.Offer.Objective), Summary: strings.TrimSpace(parsed.Offer.Summary)}
		if workflow.contractLocked {
			deps.Trace.Record("objective_contract", turnID, "planning", round, map[string]any{
				"objective_mode":   workflow.objectiveMode,
				"success_criteria": workflow.successCriteria,
			})
		}

		switch parsed.Action {
		case "complete":
			deps.Trace.Record("completion_validation", turnID, "planning", round, map[string]any{
				"objective_mode":    parsed.ObjectiveMode,
				"success_criteria":  parsed.SuccessCriteria,
				"basis_type":        parsed.CompletionBasis.Type,
				"evidence_revision": parsed.CompletionBasis.EvidenceRevision,
				"attempt_ids":       parsed.CompletionBasis.AttemptIDs,
				"admitted":          true,
			})
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision":         "complete",
				"completion_basis": parsed.CompletionBasis.Type,
			})
			answer := summary
			if parsed.ObjectiveMode == "capability" && strings.TrimSpace(parsed.Offer.Objective) != "" {
				deps.Trace.Record("pending_proposal_created", turnID, "session", -1, map[string]any{
					"objective": parsed.Offer.Objective,
					"summary":   parsed.Offer.Summary,
				})
				answer = strings.TrimSpace(answer) + "\n\nVols que ho executi?"
			}
			turnUI.Final(answer)
			completed := workflow.result(turnOutcomeCompleted, "", "")
			completed.Result = answer
			return completed, nil
		case "blocked":
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision":       "blocked",
				"blocker_kind":   parsed.BlockerKind,
				"blocker_reason": parsed.BlockerReason,
			})
			answer := strings.TrimSpace(summary)
			if answer == "" {
				answer = parsed.BlockerReason
			} else if reason := strings.TrimSpace(parsed.BlockerReason); reason != "" && reason != answer {
				answer += "\n" + reason
			}
			turnUI.Final(answer)
			blocked := workflow.result(turnOutcomeBlocked, parsed.BlockerKind, parsed.BlockerReason)
			blocked.Result = answer
			return blocked, nil
		}

		workflow.beginDecisionBatch()
		plans, rejectedPlans := workflow.admitPlans(plans)
		workflow.lastPlans = append(workflow.lastPlans[:0], plans...)
		conflictAttemptStart := len(workflow.attempts)
		workflow.recordRepetitionConflicts(rejectedPlans)
		traceWorkflowAttempts(deps, turnID, round, workflow.attempts[conflictAttemptStart:])
		for _, plan := range rejectedPlans {
			deps.Trace.Record("repeat_admission", turnID, "planning", round, map[string]any{
				"command":           plan.Command,
				"purpose":           plan.Purpose,
				"repeat_reason":     plan.RepeatReason,
				"admitted":          false,
				"evidence_revision": workflow.evidenceRevision,
			})
		}
		for _, plan := range plans {
			if plan.RepeatReason == "" {
				continue
			}
			deps.Trace.Record("repeat_admission", turnID, "planning", round, map[string]any{
				"command":           plan.Command,
				"purpose":           plan.Purpose,
				"repeat_reason":     plan.RepeatReason,
				"admitted":          true,
				"evidence_revision": workflow.evidenceRevision,
			})
		}
		if len(plans) == 0 && len(rejectedPlans) > 0 {
			workflow.stallCount++
			if workflow.stallCount == 1 {
				deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
					"decision": "repair_repetition_conflict",
				})
				continue
			}
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "no_progress",
			})
			const noProgressReason = "Shellia could not make progress because the same successful command was proposed again without an explicit repeat reason."
			turnUI.Final(noProgressReason)
			stalled := workflow.result(turnOutcomeNoProgress, "no_progress", noProgressReason)
			stalled.Result = noProgressReason
			return stalled, nil
		}

		turnUI.Plan(cfg, summary, plans, false)
		if !workflow.canExecute() {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "planned_without_execution",
			})
			return workflow.result(turnOutcomePlanned, "", ""), nil
		}

		if cfg.AskConfirmPlan {
			executePlan, err := promptPlanExecution(deps.Stdout, ui, deps.Stdin)
			if err != nil {
				return partialResult(), fmt.Errorf("cannot read plan confirmation: %w", err)
			}
			deps.Trace.Record("plan_confirmation", turnID, "", -1, map[string]any{
				"accepted": executePlan,
			})
			if !executePlan {
				printInfoTo(deps.Stdout, ui, "Plan not executed.")
				return workflow.result(turnOutcomeDeclined, "declined", "The user declined the plan."), nil
			}
		}

		attemptStart := len(workflow.attempts)
		evidenceBefore := workflow.evidenceRevision
		batch, err := deps.ExecuteCommands(ctx, deps, ui, cfg, ctxInfo, plans, workflow.executions)
		workflow.recordBatch(plans, batch)
		traceWorkflowAttempts(deps, turnID, round, workflow.attempts[attemptStart:])
		if workflow.evidenceRevision != evidenceBefore {
			deps.Trace.Record("evidence_revision", turnID, "execution", round, map[string]any{
				"before": evidenceBefore,
				"after":  workflow.evidenceRevision,
			})
		}
		if errors.Is(err, errAborted) {
			aborted := partialResult()
			aborted.Outcome = turnOutcomeDeclined
			return aborted, err
		}
		if errors.Is(err, context.Canceled) {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "execution_failure_replan_excluded",
				"reason":   "cancellation",
			})
			cancelled := partialResult()
			cancelled.Outcome = turnOutcomeCancelled
			return cancelled, err
		}
		var promptErr *interactivePromptError
		interactiveRepair := errors.As(err, &promptErr)
		if err != nil && !interactiveRepair {
			return partialResult(), err
		}

		if batch.HadTimeout {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "timeout",
			})
			timedOut := partialResult()
			timedOut.Outcome = turnOutcomeTimeout
			timedOut.BlockerKind = "timeout"
			timedOut.BlockerReason = "A command timed out; Shellia did not retry it automatically."
			return timedOut, nil
		}
		followupTrigger := "observation"
		if interactiveRepair {
			followupTrigger = "interactive_repair"
		}
		if batch.HadOrdinaryFailure {
			followupTrigger = "execution_failure"
		}

		if workflow.planningLimitReached(round) {
			keepGoing, limitErr := promptPlanningLimitContinuation(deps.Stdout, ui, deps.Stdin, workflow.planningBudget)
			if limitErr != nil {
				return partialResult(), limitErr
			}
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "planning_limit_continuation",
				"accepted": keepGoing,
				"limit":    workflow.planningBudget,
				"trigger":  followupTrigger,
			})
			if keepGoing {
				workflow.extendPlanningBudget(cfg.PlanningMaxRounds)
				if batch.HadOrdinaryFailure {
					deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
						"decision": "continue_after_execution_failure",
					})
				}
				continue
			}
			limited := partialResult()
			limited.Outcome = turnOutcomePlanningLimit
			limited.BlockerKind = "planning_limit"
			limited.BlockerReason = "The planning limit was reached and continuation was declined."
			return limited, nil
		}
		decision := "continue_after_observation"
		if batch.HadOrdinaryFailure {
			decision = "continue_after_execution_failure"
		}
		deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
			"decision": decision,
		})
		continue
	}
}

// runPlanningRound asks the model for one workflow decision and repairs one malformed response.
func runPlanningRound(ctx context.Context, request planningRoundRequest) (planningRoundResult, error) {
	cfg := request.Prompt.Config
	deps := request.Deps
	ui := request.UI

	systemPrompt, userPrompt := buildLLMPrompts(request.Prompt)
	if cfg.RawPrompt {
		printRawPromptsTo(deps.Stdout, ui, "Raw LLM prompt", systemPrompt, userPrompt)
	}

	deps.Trace.Record("llm_prompt", request.TurnID, "planning", request.Round, map[string]any{
		"model":         cfg.Model,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
	})
	deps.Trace.Record("evidence_projection", request.TurnID, "planning", request.Round, map[string]any{
		"evidence_revision": request.Prompt.EvidenceRevision,
		"executions_count":  len(request.Prompt.Observations),
		"skipped_count":     len(request.Prompt.Skipped),
		"output_budget":     cfg.ObservationOutputChars,
		"omitted": strings.Contains(userPrompt, "[older evidence omitted:") ||
			strings.Contains(userPrompt, "[older attempts omitted:") ||
			strings.Contains(userPrompt, "[omitted by configuration]"),
	})

	thinkingPrefix := ""
	if deps.Turn != nil {
		thinkingPrefix = deps.Turn.ThinkingPrefix()
	}
	thinking := startThinkingIndicator(ui, deps.Stdout, thinkingPrefix)
	rawResponse, err := callPlanningPrompt(ctx, deps.HTTPClient, cfg, systemPrompt, userPrompt)
	if thinking != nil {
		thinking.Stop()
	}
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error": err.Error(),
		})
		return planningRoundResult{}, err
	}
	deps.Trace.Record("llm_response", request.TurnID, "planning", request.Round, map[string]any{
		"raw_response": rawResponse,
	})

	if cfg.RawResponse {
		printSectionTo(deps.Stdout, ui, "Raw LLM response", colorBlue)
		fmt.Fprintln(deps.Stdout, rawResponse)
		fmt.Fprintln(deps.Stdout)
	}

	parsed, err := parseResponse(rawResponse)
	structuralRepairUsed := false
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error":        err.Error(),
			"raw_response": rawResponse,
		})
		if !request.AllowStructuralRepair {
			return planningRoundResult{}, fmt.Errorf("%w: %v", errStructuralResponse, err)
		}
		structuralRepairUsed = true
		repairPrompt := userPrompt + "\n\nThe previous response was structurally invalid: " + err.Error() + "\nReturn exactly one valid JSON decision using the required schema."
		deps.Trace.Record("llm_prompt", request.TurnID, "structural_repair", request.Round, map[string]any{
			"model":         cfg.Model,
			"system_prompt": systemPrompt,
			"user_prompt":   repairPrompt,
		})
		repairedRaw, repairErr := callPlanningPrompt(ctx, deps.HTTPClient, cfg, systemPrompt, repairPrompt)
		if repairErr != nil {
			return planningRoundResult{}, repairErr
		}
		deps.Trace.Record("llm_response", request.TurnID, "structural_repair", request.Round, map[string]any{
			"raw_response": repairedRaw,
		})
		parsed, err = parseResponse(repairedRaw)
		if err != nil {
			return planningRoundResult{}, fmt.Errorf("%w: %v", errStructuralResponse, err)
		}
	}

	summary, plans, err := normalizePlan(parsed)
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error":  err.Error(),
			"parsed": parsed,
		})
		return planningRoundResult{}, err
	}
	deps.Trace.Record("planner_result", request.TurnID, "planning", request.Round, map[string]any{
		"action":           parsed.Action,
		"summary":          summary,
		"completion_basis": parsed.CompletionBasis,
		"blocker_kind":     parsed.BlockerKind,
		"blocker_reason":   parsed.BlockerReason,
		"commands":         tracePlannerCommands(plans),
		"commands_count":   len(plans),
	})

	return planningRoundResult{
		Parsed:               parsed,
		Summary:              summary,
		Plans:                plans,
		StructuralRepairUsed: structuralRepairUsed,
	}, nil
}

// tracePlannerCommands renders normalized command plans with stable trace field names.
func tracePlannerCommands(plans []commandPlan) []map[string]any {
	commands := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		commands = append(commands, map[string]any{
			"command":                plan.Command,
			"purpose":                plan.Purpose,
			"risk":                   plan.Risk,
			"requires_confirmation":  plan.RequiresConfirmation,
			"classification":         plan.Classification,
			"local_safe":             plan.LocalSafe,
			"independent_on_failure": plan.IndependentOnFailure,
			"repeat_reason":          plan.RepeatReason,
			"interactive":            plan.Interactive,
			"interactive_reason":     plan.InteractiveReason,
		})
	}
	return commands
}

// traceWorkflowAttempts records causal command outcomes without duplicating captured output.
func traceWorkflowAttempts(deps runtimeDeps, turnID string, round int, attempts []workflowAttempt) {
	for _, attempt := range attempts {
		deps.Trace.Record("workflow_attempt", turnID, "execution", round, map[string]any{
			"attempt_id":         attempt.ID,
			"planned_command":    attempt.PlannedCommand,
			"effective_command":  attempt.EffectiveCommand,
			"purpose":            attempt.Purpose,
			"outcome":            attempt.Outcome,
			"exit_code":          attempt.ExitCode,
			"repeat_reason":      attempt.RepeatReason,
			"related_attempt_id": attempt.RelatedAttemptID,
			"evidence_before":    attempt.EvidenceBefore,
			"evidence_after":     attempt.EvidenceAfter,
		})
	}
}
