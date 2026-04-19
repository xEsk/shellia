package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
		if err := initConfigFile(ui); err != nil {
			exitWithError(ui, err.Error(), 1)
		}
		return
	case "config-path":
		path, err := settingsPath()
		if err != nil {
			exitWithError(ui, err.Error(), 1)
		}
		renderPanel(os.Stdout, ui, "config", colorCyan, []string{path})
		return
	}

	ctxInfo, err := getContext()
	if err != nil {
		exitWithError(ui, err.Error(), 1)
	}

	// appCtx is cancelled on the first Ctrl+C, aborting any in-flight LLM request.
	appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if cfg.Interactive {
		runInteractive(appCtx, ui, cfg, &ctxInfo)
		return
	}

	_, err = runTurn(appCtx, ui, cfg, &ctxInfo, cfg.Instruction, nil, sessionState{})
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
	cfg := loadBaseConfig()

	if kind, ok := parseConfigSubcommand(args); ok {
		cfg.CommandKind = kind
		return cfg, nil
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
func loadBaseConfig() config {
	cfg := defaultConfig()
	fileCfg, fileErr := loadFileConfig()
	applyFileConfig(&cfg, fileCfg, fileErr)
	applyEnvConfig(&cfg)
	return cfg
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
	fs.IntVar(&timeoutSecs, "timeout", timeoutSecs, "per-command timeout in seconds")
	fs.IntVar(&reqTimeoutSecs, "request-timeout", reqTimeoutSecs, "HTTP request timeout in seconds")
	fs.BoolVar(&cfg.YesSafe, "yes-safe", cfg.YesSafe, "auto-execute safe commands without confirmation")
	fs.BoolVar(&cfg.ContinueOnError, "continue-on-error", cfg.ContinueOnError, "continue if a command fails")
	fs.BoolVar(&cfg.Interactive, "interactive", false, "start or maintain an interactive session")
	fs.BoolVar(&cfg.Interactive, "i", false, "short alias for --interactive")
	fs.BoolVar(&cfg.Debug, "debug", cfg.Debug, "show context and debug data")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "show full plan and technical detail")
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
	cfg.CommandTimeout = time.Duration(timeoutSecs) * time.Second
	cfg.RequestTimeout = time.Duration(reqTimeoutSecs) * time.Second

	remaining := fs.Args()
	if len(remaining) == 0 {
		cfg.Interactive = true
		if strings.TrimSpace(cfg.APIKey) == "" {
			return config{}, fmt.Errorf("missing API key. Use --api-key or set SHELLIA_API_KEY")
		}
		return cfg, nil
	}

	cfg.Instruction = strings.Join(remaining, " ")

	if cfg.CommandKind != "" {
		return cfg, nil
	}

	if strings.TrimSpace(cfg.APIKey) == "" {
		return config{}, fmt.Errorf("missing API key. Use --api-key or set SHELLIA_API_KEY")
	}

	return cfg, nil
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
func runInteractive(ctx context.Context, ui bool, cfg config, ctxInfo *contextInfo) {
	reader := bufio.NewReader(os.Stdin)
	history := make([]historyEntry, 0, maxHistoryEntries)
	state := sessionState{}
	mode := interactiveModeAI

	printSessionBanner(ui)

	if strings.TrimSpace(cfg.Instruction) != "" {
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, ui, cfg, ctxInfo, cfg.Instruction, history, state)
		stop()
		if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
			state.LastRetryInstruction = cfg.Instruction
			rememberUnfinishedInstruction(&state, cfg.Instruction)
		} else if err != nil {
			printWarning(ui, err.Error())
			state.LastRetryInstruction = cfg.Instruction
			rememberUnfinishedInstruction(&state, cfg.Instruction)
		} else {
			history = append(history, historyEntry{Instruction: cfg.Instruction, Result: turn.Result})
			updateSessionState(&state, cfg.Instruction, turn)
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

		input, err := readInteractivePrompt(ui, reader, mode, cfg.ShowCommandPopup)
		if err != nil {
			exitWithError(ui, fmt.Sprintf("cannot read prompt: %v", err), 1)
		}

		trimmed := strings.TrimSpace(input)
		command := parseInteractiveCommand(trimmed)
		if command != interactiveCommandNone {
			switch command {
			case interactiveCommandUnknown:
				printWarning(ui, "Unknown command: "+trimmed)
				continue
			case interactiveCommandExit:
				fmt.Println()
				printInfo(ui, "Session closed.")
				return
			case interactiveCommandClear:
				clearScreen()
				continue
			case interactiveCommandContext:
				printContext(ui, *ctxInfo)
				continue
			case interactiveCommandShell:
				mode = interactiveModeShell
				printModeStatus(ui, fmt.Sprintf("Shell mode enabled (%s).", cfg.ShellMode))
				continue
			case interactiveCommandAI:
				mode = interactiveModeAI
				printModeStatus(ui, "Prompt mode enabled.")
				continue
			case interactiveCommandMode:
				printModeStatus(ui, "Current mode: "+string(mode))
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
				printWarning(ui, "Missing shell command.")
				continue
			}

			turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
			execution, err := executeManualCommand(turnCtx, ui, cfg, ctxInfo, command, renderMode)
			stop()

			if errors.Is(err, context.Canceled) {
				printWarning(ui, "Command cancelled.")
				continue
			}
			if err != nil {
				printWarning(ui, err.Error())
				continue
			}

			updateSessionStateFromExecution(&state, command, execution)
			continue
		}

		instruction := input
		if isRetryInstruction(input) && strings.TrimSpace(state.LastRetryInstruction) != "" {
			instruction = state.LastRetryInstruction
			printInfo(ui, fmt.Sprintf("Retrying: %s", instruction))
		}

		// Per-turn signal context: Ctrl+C cancels only this turn, not the whole session.
		turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		turn, err := runTurn(turnCtx, ui, cfg, ctxInfo, instruction, history, state)
		stop()

		if errors.Is(err, errAborted) || errors.Is(err, context.Canceled) {
			state.LastRetryInstruction = instruction
			rememberUnfinishedInstruction(&state, instruction)
			printWarning(ui, "Request cancelled.")
			fmt.Println()
			printSeparator(os.Stdout, ui)
			continue
		}
		if err != nil {
			printWarning(ui, err.Error())
			state.LastRetryInstruction = instruction
			rememberUnfinishedInstruction(&state, instruction)
			continue
		}
		history = append(history, historyEntry{Instruction: instruction, Result: turn.Result})
		updateSessionState(&state, instruction, turn)
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

// runTurn executes a full plan → confirm → execute → answer cycle.
func runTurn(ctx context.Context, ui bool, cfg config, ctxInfo *contextInfo, instruction string, history []historyEntry, state sessionState) (turnResult, error) {
	if cfg.Debug || cfg.Verbose {
		printContext(ui, *ctxInfo)
	}

	printHeader(ui, *ctxInfo)
	allExecutions := make([]commandExecution, 0, 4)
	lastSummary := ""
	lastPlans := []commandPlan(nil)

	for round := 0; round < maxPlanRounds; round++ {
		thinking := startThinkingIndicator(ui, os.Stdout)
		rawResponse, err := callLLM(ctx, cfg, *ctxInfo, instruction, history, state, allExecutions)
		if thinking != nil {
			thinking.stop()
		}
		if err != nil {
			return turnResult{}, err
		}

		if cfg.RawResponse {
			printSection(ui, "Raw LLM response", colorBlue)
			fmt.Println(rawResponse)
			fmt.Println()
		}

		parsed, err := parseResponse(rawResponse)
		if err != nil {
			return turnResult{}, err
		}

		summary, plans, err := normalizePlan(parsed)
		if err != nil {
			return turnResult{}, err
		}

		if len(plans) == 0 && shouldRetryWithDiscoveryRepair(parsed, round, allExecutions) {
			thinking = startThinkingIndicator(ui, os.Stdout)
			repairedRawResponse, repairErr := callDiscoveryRepairLLM(ctx, cfg, *ctxInfo, instruction, history, state, allExecutions, parsed)
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
			printFinalResult(ui, summary)
			return turnResult{Result: summary, Summary: summary, Actionable: false, Plans: plans}, nil
		}

		if shouldSkipRedundantRound(plans, allExecutions) {
			break
		}

		printPlan(ui, cfg, summary, plans, parsed.RequiresObservation)
		executions, err := executeCommands(ctx, ui, cfg, ctxInfo, plans)
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

	openResultPanel(ui)
	w := &resultWriter{ui: ui, thinking: startThinkingIndicator(ui, os.Stdout)}
	result, streamErr := streamSummarizeExecutions(ctx, cfg, instruction, allExecutions, w)
	w.stopThinking()
	if streamErr != nil || strings.TrimSpace(result) == "" {
		result = staticFallbackAnswer(lastSummary, allExecutions)
		// Only print the fallback if streaming never wrote a single byte to the terminal.
		// If it wrote partial content before erroring, don't print on top of it.
		if !w.wroteAnything {
			if err := renderAnswerBlock(os.Stdout, ui, result, &w.state); err != nil {
				return turnResult{}, err
			}
			w.wroteAnything = true
		}
	}
	closeResultPanel(ui)
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
