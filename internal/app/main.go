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

// planningRoundRequest groups one model planning attempt and its rendering dependencies.
type planningRoundRequest struct {
	Deps   runtimeDeps
	UI     bool
	TurnID string
	Round  int
	Prompt llmPromptRequest
}

// planningRoundResult is the normalized output of one planning attempt.
type planningRoundResult struct {
	Parsed  llmResponse
	Summary string
	Plans   []commandPlan
}

// turnRequest groups the context needed to process one user instruction.
type turnRequest struct {
	Config      config
	ContextInfo *contextInfo
	Instruction string
	History     []historyEntry
	State       sessionState
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

	ui := uiEnabled(cfg)

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

	_, err = runTurn(appCtx, deps, ui, turnRequest{
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
	return 0
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
	fs.BoolVar(&cfg.AskConfirmPlanOnly, "ask-confirm-plan-only", cfg.AskConfirmPlanOnly, "ask for confirmation after showing the plan in plan-only mode")
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
	if cfg.ObservationOutputChars > maxOutputChars || cfg.SummaryOutputChars > maxOutputChars {
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
		if err != nil && turn.Actionable {
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
			if turn.Actionable {
				state.LastRetryInstruction = ""
			}
		}
	}

	for {
		// Check if the parent context was cancelled (e.g. second Ctrl+C).
		if ctx.Err() != nil {
			return
		}

		input, err := readInteractivePrompt(ui, reader, deps.Stdin, deps.Stdout, mode, cfg)
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
			if err != nil && turn.Actionable {
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

		// Per-turn signal context: Ctrl+C cancels only this turn, not the whole session.
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, deps, ui, turnRequest{
			Config:      cfg,
			ContextInfo: ctxInfo,
			Instruction: instruction,
			History:     history,
			State:       state,
		})
		stop()
		if err != nil && turn.Actionable {
			updateSessionState(&state, instruction, turn, cfg)
		}

		if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
			state.LastRetryInstruction = instruction
			rememberUnfinishedInstruction(&state, instruction)
			printWarningTo(deps.Stderr, ui, "Request cancelled.")
			fmt.Fprintln(deps.Stdout)
			printSeparator(deps.Stdout, ui)
			continue
		}
		if err != nil {
			printWarningTo(deps.Stderr, ui, err.Error())
			state.LastRetryInstruction = instruction
			rememberUnfinishedInstruction(&state, instruction)
			continue
		}
		history = append(history, historyEntry{Instruction: instruction, Result: turn.Result})
		updateSessionState(&state, instruction, turn, cfg)
		if turn.Actionable {
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

// runTurn executes a full plan → confirm → execute → answer cycle, or stops after planning in plan-only mode.
func runTurn(ctx context.Context, deps runtimeDeps, ui bool, request turnRequest) (result turnResult, err error) {
	deps = deps.withDefaults()
	cfg := request.Config
	ctxInfo := request.ContextInfo
	instruction := request.Instruction
	history := request.History
	state := request.State
	turnID := deps.Trace.StartTurn(map[string]any{
		"instruction":   instruction,
		"cwd":           ctxInfo.CWD,
		"history_count": len(history),
		"state":         state,
	})
	ctx = withTraceTurnID(ctx, turnID)
	defer func() {
		data := map[string]any{
			"result":           result.Result,
			"actionable":       result.Actionable,
			"plans_count":      len(result.Plans),
			"executions_count": len(result.Executions),
		}
		if err != nil {
			data["error"] = err.Error()
		}
		deps.Trace.Record("turn_end", turnID, "", -1, data)
	}()

	if cfg.Debug || cfg.Verbose {
		printContextTo(deps.Stdout, ui, cfg, *ctxInfo)
	}

	printHeaderTo(deps.Stdout, ui, cfg, *ctxInfo)
	allExecutions := make([]commandExecution, 0, 4)
	allSkipped := make([]skippedCommand, 0, 4)
	lastSummary := ""
	lastPlans := []commandPlan(nil)
	planningRoundLimit := cfg.PlanningMaxRounds
	partialResult := func() turnResult {
		return turnResult{
			Result:     strings.TrimSpace(lastSummary),
			Summary:    lastSummary,
			Actionable: len(allExecutions) > 0,
			Plans:      lastPlans,
			Executions: allExecutions,
			Skipped:    allSkipped,
		}
	}

	for round := 0; ; round++ {
		promptRequest := llmPromptRequest{
			Config:       cfg,
			ContextInfo:  *ctxInfo,
			Instruction:  instruction,
			History:      history,
			State:        state,
			Observations: allExecutions,
			Skipped:      allSkipped,
		}
		roundResult, err := runPlanningRound(ctx, planningRoundRequest{
			Deps:   deps,
			UI:     ui,
			TurnID: turnID,
			Round:  round,
			Prompt: promptRequest,
		})
		if err != nil {
			return partialResult(), err
		}
		parsed := roundResult.Parsed
		summary := roundResult.Summary
		plans := roundResult.Plans

		lastSummary = summary
		lastPlans = plans

		if len(plans) == 0 {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "final_answer_without_commands",
			})
			printFinalResultTo(deps.Stdout, ui, summary)
			if cfg.PlanOnly {
				printPlanOnlyGuidanceTo(deps.Stdout, ui, parsed, cfg.AskConfirmPlanOnly)
			}
			return turnResult{
				Result:     summary,
				Summary:    summary,
				Actionable: len(allExecutions) > 0,
				Plans:      plans,
				Executions: allExecutions,
				Skipped:    allSkipped,
			}, nil
		}

		if shouldSkipRedundantRound(plans, allExecutions) {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "skip_redundant_round",
			})
			break
		}

		printPlanTo(deps.Stdout, ui, cfg, summary, plans, parsed.RequiresObservation)
		if cfg.PlanOnly {
			printPlanOnlyGuidanceTo(deps.Stdout, ui, parsed, cfg.AskConfirmPlanOnly)
		}

		skipConfirm := (cfg.PlanOnly && !cfg.AskConfirmPlanOnly) || (!cfg.PlanOnly && !cfg.AskConfirmPlan)
		if skipConfirm {
			if cfg.PlanOnly {
				return turnResult{Result: planOnlyResult(summary, parsed), Summary: summary, Actionable: len(plans) > 0, Plans: plans}, nil
			}
		} else {
			executePlan, err := promptPlanExecution(deps.Stdout, ui, deps.Stdin)
			if err != nil {
				return partialResult(), fmt.Errorf("cannot read plan confirmation: %w", err)
			}
			deps.Trace.Record("plan_confirmation", turnID, "", -1, map[string]any{
				"accepted": executePlan,
			})
			if !executePlan {
				if cfg.PlanOnly {
					return turnResult{Result: planOnlyResult(summary, parsed), Summary: summary, Actionable: len(plans) > 0, Plans: plans}, nil
				}
				printInfoTo(deps.Stdout, ui, "Plan not executed.")
				return turnResult{
					Result:     summary,
					Summary:    summary,
					Actionable: len(allExecutions) > 0,
					Plans:      plans,
					Executions: allExecutions,
					Skipped:    allSkipped,
				}, nil
			}
		}

		if cfg.PlanOnly {
			cfg.PlanOnly = false
		}
		batch, err := deps.ExecuteCommands(ctx, deps, ui, cfg, ctxInfo, plans)
		allExecutions = append(allExecutions, batch.Executions...)
		allSkipped = append(allSkipped, batch.Skipped...)
		if errors.Is(err, errAborted) {
			return partialResult(), err
		}
		if errors.Is(err, context.Canceled) {
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "execution_failure_replan_excluded",
				"reason":   "cancellation",
			})
			return partialResult(), err
		}
		var promptErr *interactivePromptError
		interactiveRepair := errors.As(err, &promptErr)
		if err != nil && !interactiveRepair {
			return partialResult(), err
		}

		requiresFollowup := interactiveRepair || batch.HadOrdinaryFailure || (parsed.RequiresObservation && !batch.HadTimeout)
		if !requiresFollowup {
			if batch.HadTimeout {
				deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
					"decision": "execution_failure_replan_excluded",
					"reason":   "timeout",
				})
			}
			break
		}

		if round >= planningRoundLimit-1 {
			keepGoing, limitErr := promptPlanningLimitContinuation(deps.Stdout, ui, deps.Stdin, planningRoundLimit)
			if limitErr != nil {
				return partialResult(), limitErr
			}
			deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
				"decision": "planning_limit_continuation",
				"accepted": keepGoing,
				"limit":    planningRoundLimit,
			})
			if keepGoing {
				planningRoundLimit += cfg.PlanningMaxRounds
				continue
			}
			break
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

	openResultPanelTo(deps.Stdout, ui)
	w := newResultWriter(ui, deps.Stdout)
	answer, streamErr := streamSummarizeExecutions(ctx, deps.HTTPClient, cfg, instruction, allExecutions, allSkipped, w, deps.Trace, turnID)
	w.StopThinking()
	if ctx.Err() != nil {
		closeResultPanelTo(deps.Stdout, ui)
		return partialResult(), ctx.Err()
	}
	if streamErr != nil || strings.TrimSpace(answer) == "" {
		answer = staticFallbackAnswer(lastSummary, allExecutions)
		// Only print the fallback if streaming never wrote a single byte to the terminal.
		// If it wrote partial content before erroring, don't print on top of it.
		if !w.WroteAnything() {
			if err := renderAnswerBlock(deps.Stdout, ui, answer, w.AnswerState()); err != nil {
				return partialResult(), err
			}
			w.MarkWroteAnything()
		}
	}
	closeResultPanelTo(deps.Stdout, ui)
	return turnResult{
		Result:     strings.TrimSpace(answer),
		Summary:    lastSummary,
		Actionable: true,
		Plans:      lastPlans,
		Executions: allExecutions,
		Skipped:    allSkipped,
	}, nil
}

// runPlanningRound asks the model for one plan and applies discovery repair when useful.
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

	thinking := startThinkingIndicator(ui, deps.Stdout)
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
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error":        err.Error(),
			"raw_response": rawResponse,
		})
		return planningRoundResult{}, err
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
		"summary":              summary,
		"requires_input":       parsed.RequiresInput,
		"input_reason":         parsed.InputReason,
		"requires_observation": parsed.RequiresObservation,
		"observation_reason":   parsed.ObservationReason,
		"commands":             plans,
		"commands_count":       len(plans),
	})

	if len(plans) == 0 && !cfg.PlanOnly && shouldRetryWithDiscoveryRepair(parsed, request.Round, request.Prompt.Observations) {
		deps.Trace.Record("shellia_decision", request.TurnID, "planning", request.Round, map[string]any{
			"decision": "discovery_repair_triggered",
		})
		repaired, ok := runDiscoveryRepair(ctx, request, parsed)
		if ok {
			return repaired, nil
		}
		deps.Trace.Record("shellia_decision", request.TurnID, "planning", request.Round, map[string]any{
			"decision": "discovery_repair_failed",
		})
	}

	return planningRoundResult{
		Parsed:  parsed,
		Summary: summary,
		Plans:   plans,
	}, nil
}

// runDiscoveryRepair tries one discovery-only retry after a recoverable empty plan.
func runDiscoveryRepair(
	ctx context.Context,
	request planningRoundRequest,
	previous llmResponse,
) (planningRoundResult, bool) {
	cfg := request.Prompt.Config
	deps := request.Deps
	ui := request.UI
	repairRequest := discoveryPromptRequest{
		Prompt:   request.Prompt,
		Previous: previous,
	}

	systemPrompt, userPrompt := buildDiscoveryRepairLLMPrompts(repairRequest)
	if cfg.RawPrompt {
		printRawPromptsTo(deps.Stdout, ui, "Raw discovery repair prompt", systemPrompt, userPrompt)
	}
	deps.Trace.Record("llm_prompt", request.TurnID, "discovery_repair", request.Round, map[string]any{
		"model":         cfg.Model,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
	})

	thinking := startThinkingIndicator(ui, deps.Stdout)
	repairedRawResponse, repairErr := callPlanningPrompt(ctx, deps.HTTPClient, cfg, systemPrompt, userPrompt)
	if thinking != nil {
		thinking.Stop()
	}
	if repairErr != nil {
		deps.Trace.Record("llm_error", request.TurnID, "discovery_repair", request.Round, map[string]any{
			"error": repairErr.Error(),
		})
		return planningRoundResult{}, false
	}
	deps.Trace.Record("llm_response", request.TurnID, "discovery_repair", request.Round, map[string]any{
		"raw_response": repairedRawResponse,
	})

	repairedParsed, parseErr := parseResponse(repairedRawResponse)
	if parseErr != nil {
		deps.Trace.Record("llm_error", request.TurnID, "discovery_repair", request.Round, map[string]any{
			"error":        parseErr.Error(),
			"raw_response": repairedRawResponse,
		})
		return planningRoundResult{}, false
	}

	repairedSummary, repairedPlans, normalizeErr := normalizePlan(repairedParsed)
	if normalizeErr != nil {
		deps.Trace.Record("llm_error", request.TurnID, "discovery_repair", request.Round, map[string]any{
			"error":  normalizeErr.Error(),
			"parsed": repairedParsed,
		})
		return planningRoundResult{}, false
	}
	deps.Trace.Record("planner_result", request.TurnID, "discovery_repair", request.Round, map[string]any{
		"summary":              repairedSummary,
		"requires_input":       repairedParsed.RequiresInput,
		"input_reason":         repairedParsed.InputReason,
		"requires_observation": repairedParsed.RequiresObservation,
		"observation_reason":   repairedParsed.ObservationReason,
		"commands":             repairedPlans,
		"commands_count":       len(repairedPlans),
	})

	return planningRoundResult{
		Parsed:  repairedParsed,
		Summary: repairedSummary,
		Plans:   repairedPlans,
	}, true
}

// shouldSkipRedundantRound avoids re-running commands already executed in the same turn.
func shouldSkipRedundantRound(plans []commandPlan, executions []commandExecution) bool {
	if len(plans) == 0 || len(executions) == 0 {
		return false
	}

	executedCommands := make(map[string]bool, len(executions))
	for _, execution := range executions {
		command := strings.TrimSpace(execution.Command)
		if command != "" {
			executedCommands[command] = true
		}
	}

	for _, plan := range plans {
		if !executedCommands[strings.TrimSpace(plan.Command)] {
			return false
		}
	}

	return true
}
