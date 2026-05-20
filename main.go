package main

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
	defaultBaseURL    = "https://api.openai.com/v1"
	defaultModel      = "gpt-5.4-mini"
	defaultTimeout    = 120 * time.Second
	maxHistoryEntries = 8
	maxPlanRounds     = 4

	maxCommandTimeout = 24 * time.Hour
	maxRequestTimeout = 10 * time.Minute
	maxCaptureBytes   = 512 * 1024 * 1024 // 512 MB
	maxOutputChars    = 100_000
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

var errAborted = errors.New("aborted by user")
var errHelp = errors.New("help requested")

type contextInfo struct {
	CWD   string     `json:"cwd"`
	User  string     `json:"user"`
	OS    string     `json:"os"`
	Shell string     `json:"shell"`
	Git   gitContext `json:"git"`
}

type gitContext struct {
	IsRepo      bool   `json:"is_repo"`
	Branch      string `json:"branch"`
	StatusShort string `json:"status_short"`
}

type historyEntry struct {
	Instruction string
	Result      string
}

type interactiveMode string

const (
	interactiveModeAI    interactiveMode = "ai"
	interactiveModeShell interactiveMode = "shell"
)

type observationMemory struct {
	Command    string
	Purpose    string
	Transcript string
}

type sessionState struct {
	LastRetryInstruction string
	PendingIntent        string
	LastCreatedFiles     []string
	LastRuntimeHint      string
	LastReferencedFile   string
	LastObservations     []observationMemory
	LastSuggestedCommand string
}

type turnResult struct {
	Result     string
	Summary    string
	Actionable bool
	Plans      []commandPlan
	Executions []commandExecution
}

func main() {
	deps := defaultRuntimeDeps()

	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			return
		}
		exitWithError(uiEnabled(config{}), err.Error(), 2)
	}

	ui := uiEnabled(cfg)

	switch cfg.CommandKind {
	case "config-init":
		if err := initConfigFileTo(deps.Stdout, ui); err != nil {
			exitWithError(ui, err.Error(), 1)
		}
		return
	case "config-path":
		path, err := settingsPath()
		if err != nil {
			exitWithError(ui, err.Error(), 1)
		}
		renderPanel(deps.Stdout, ui, "config", colorCyan, []string{path})
		return
	}

	ctxInfo, err := getContext(cfg)
	if err != nil {
		exitWithError(ui, err.Error(), 1)
	}

	// appCtx is cancelled on the first Ctrl+C, aborting any in-flight LLM request.
	appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if cfg.Interactive {
		runInteractive(appCtx, deps, ui, cfg, &ctxInfo)
		return
	}

	_, err = runTurn(appCtx, deps, ui, cfg, &ctxInfo, cfg.Instruction, nil, sessionState{})
	if err != nil {
		switch {
		case errors.Is(err, errAborted), errors.Is(err, context.Canceled):
			exitWithError(ui, "execution aborted", 130)
		default:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code := exitErr.ExitCode()
				if code <= 0 {
					code = 1
				}
				exitWithError(ui, err.Error(), code)
			}
			exitWithError(ui, err.Error(), 1)
		}
	}
}

// parseArgs processes CLI config and validates the minimum required values.
func parseArgs(args []string) (config, error) {
	if kind, ok := parseConfigSubcommand(args); ok {
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
		return config{}, fmt.Errorf("invalid arguments")
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
func parseConfigSubcommand(args []string) (string, bool) {
	if len(args) < 2 || args[0] != "config" {
		return "", false
	}
	switch args[1] {
	case "init":
		return "config-init", true
	case "path":
		return "config-path", true
	}
	return "", false
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

	cfg.CommandTimeout = time.Duration(timeoutSecs) * time.Second
	cfg.RequestTimeout = time.Duration(reqTimeoutSecs) * time.Second

	if cfg.CommandTimeout > maxCommandTimeout {
		return config{}, fmt.Errorf("timeout_seconds too large (max %v)", maxCommandTimeout)
	}
	if cfg.RequestTimeout > maxRequestTimeout {
		return config{}, fmt.Errorf("request_timeout_seconds too large (max %v)", maxRequestTimeout)
	}
	if cfg.CaptureStdoutBytes > maxCaptureBytes || cfg.CaptureStderrBytes > maxCaptureBytes {
		return config{}, fmt.Errorf("capture byte limits cannot exceed %d bytes", maxCaptureBytes)
	}
	if cfg.ObservationOutputChars > maxOutputChars || cfg.SummaryOutputChars > maxOutputChars {
		return config{}, fmt.Errorf("output char limits cannot exceed %d", maxOutputChars)
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
		return config{}, fmt.Errorf("missing model configuration. Configure [[models]] or pass --base-url and --model")
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
	return fmt.Errorf("missing API key. Use --api-key or set SHELLIA_API_KEY")
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
		turn, err := runTurn(turnCtx, deps, ui, cfg, ctxInfo, cfg.Instruction, history, state)
		stop()
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
		planOnly, plannedInstruction := parsePlanInstruction(input)
		if planOnly {
			if strings.TrimSpace(plannedInstruction) == "" {
				printWarningTo(deps.Stderr, ui, "Missing plan instruction.")
				continue
			}

			turnCfg := cfg
			turnCfg.PlanOnly = true
			turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			turn, err := runTurn(turnCtx, deps, ui, turnCfg, ctxInfo, plannedInstruction, history, state)
			stop()

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
			}
		}

		if trimmed == "" {
			continue
		}

		if mode == interactiveModeShell || strings.HasPrefix(trimmed, "!") {
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
		if isRetryInstruction(input) && strings.TrimSpace(state.LastRetryInstruction) != "" {
			instruction = state.LastRetryInstruction
			printInfoTo(deps.Stdout, ui, fmt.Sprintf("Retrying: %s", instruction))
		}

		// Per-turn signal context: Ctrl+C cancels only this turn, not the whole session.
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, deps, ui, cfg, ctxInfo, instruction, history, state)
		stop()

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
func runTurn(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, instruction string, history []historyEntry, state sessionState) (turnResult, error) {
	deps = deps.withDefaults()
	if cfg.Debug || cfg.Verbose {
		printContextTo(deps.Stdout, ui, cfg, *ctxInfo)
	}

	printHeaderTo(deps.Stdout, ui, cfg, *ctxInfo)
	allExecutions := make([]commandExecution, 0, 4)
	lastSummary := ""
	lastPlans := []commandPlan(nil)

	for round := 0; round < maxPlanRounds; round++ {
		if cfg.RawPrompt {
			systemPrompt, userPrompt := buildLLMPrompts(cfg, *ctxInfo, instruction, history, state, allExecutions)
			printRawPromptsTo(deps.Stdout, ui, "Raw LLM prompt", systemPrompt, userPrompt)
		}

		thinking := startThinkingIndicator(ui, deps.Stdout)
		rawResponse, err := callLLM(ctx, deps.HTTPClient, cfg, *ctxInfo, instruction, history, state, allExecutions)
		if thinking != nil {
			thinking.stop()
		}
		if err != nil {
			return turnResult{}, err
		}

		if cfg.RawResponse {
			printSectionTo(deps.Stdout, ui, "Raw LLM response", colorBlue)
			fmt.Fprintln(deps.Stdout, rawResponse)
			fmt.Fprintln(deps.Stdout)
		}

		parsed, err := parseResponse(rawResponse)
		if err != nil {
			return turnResult{}, err
		}

		summary, plans, err := normalizePlan(parsed)
		if err != nil {
			return turnResult{}, err
		}

		if len(plans) == 0 && !cfg.PlanOnly && shouldRetryWithDiscoveryRepair(parsed, round, allExecutions) {
			if cfg.RawPrompt {
				systemPrompt, userPrompt := buildDiscoveryRepairLLMPrompts(cfg, *ctxInfo, instruction, history, state, allExecutions, parsed)
				printRawPromptsTo(deps.Stdout, ui, "Raw discovery repair prompt", systemPrompt, userPrompt)
			}

			thinking = startThinkingIndicator(ui, deps.Stdout)
			repairedRawResponse, repairErr := callDiscoveryRepairLLM(ctx, deps.HTTPClient, cfg, *ctxInfo, instruction, history, state, allExecutions, parsed)
			if thinking != nil {
				thinking.stop()
			}
			if repairErr == nil {
				repairedParsed, parseErr := parseResponse(repairedRawResponse)
				if parseErr == nil {
					repairedSummary, repairedPlans, normalizeErr := normalizePlan(repairedParsed)
					if normalizeErr == nil {
						rawResponse = repairedRawResponse
						parsed = repairedParsed
						summary = repairedSummary
						plans = repairedPlans
					}
				}
			}
		}

		lastSummary = summary
		lastPlans = plans

		if len(plans) == 0 {
			printFinalResultTo(deps.Stdout, ui, summary)
			if cfg.PlanOnly {
				printPlanOnlyGuidanceTo(deps.Stdout, ui, parsed, cfg.AskConfirmPlanOnly)
			}
			return turnResult{Result: summary, Summary: summary, Actionable: false, Plans: plans}, nil
		}

		if shouldSkipRedundantRound(plans, allExecutions) {
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
				return turnResult{}, fmt.Errorf("cannot read plan confirmation: %w", err)
			}
			if !executePlan {
				if cfg.PlanOnly {
					return turnResult{Result: planOnlyResult(summary, parsed), Summary: summary, Actionable: len(plans) > 0, Plans: plans}, nil
				}
				printInfoTo(deps.Stdout, ui, "Plan not executed.")
				return turnResult{Result: summary, Summary: summary, Actionable: false, Plans: plans}, nil
			}
		}

		if cfg.PlanOnly {
			cfg.PlanOnly = false
		}
		executions, err := deps.ExecuteCommands(ctx, deps, ui, cfg, ctxInfo, plans)
		if err != nil {
			if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
				return turnResult{}, err
			}
			if len(executions) == 0 {
				return turnResult{}, err
			}
			allExecutions = append(allExecutions, executions...)
			if shouldRetryAfterExecutionError(err, round) {
				continue
			}
			if parsed.RequiresObservation {
				if round == maxPlanRounds-1 {
					return turnResult{}, fmt.Errorf("planning needs more follow-up rounds than allowed")
				}
				continue
			}
			break
		}
		allExecutions = append(allExecutions, executions...)

		if !parsed.RequiresObservation {
			break
		}

		if round == maxPlanRounds-1 {
			return turnResult{}, fmt.Errorf("planning needs more follow-up rounds than allowed")
		}
	}

	openResultPanelTo(deps.Stdout, ui)
	w := &resultWriter{ui: ui, target: deps.Stdout, thinking: startThinkingIndicator(ui, deps.Stdout)}
	result, streamErr := streamSummarizeExecutions(ctx, deps.HTTPClient, cfg, instruction, allExecutions, w)
	w.stopThinking()
	if streamErr != nil || strings.TrimSpace(result) == "" {
		result = staticFallbackAnswer(lastSummary, allExecutions)
		// Only print the fallback if streaming never wrote a single byte to the terminal.
		// If it wrote partial content before erroring, don't print on top of it.
		if !w.wroteAnything {
			if err := renderAnswerBlock(deps.Stdout, ui, result, &w.state); err != nil {
				return turnResult{}, err
			}
			w.wroteAnything = true
		}
	}
	closeResultPanelTo(deps.Stdout, ui)
	return turnResult{
		Result:     strings.TrimSpace(result),
		Summary:    lastSummary,
		Actionable: true,
		Plans:      lastPlans,
		Executions: allExecutions,
	}, nil
}

// shouldRetryAfterExecutionError reports whether an execution failure should become a new planning observation.
func shouldRetryAfterExecutionError(err error, round int) bool {
	if round >= maxPlanRounds-1 {
		return false
	}

	var promptErr *interactivePromptError
	return errors.As(err, &promptErr)
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
