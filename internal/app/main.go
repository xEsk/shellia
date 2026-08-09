package app

import (
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

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
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

// SetVersion records the binary version for UI and trace metadata.
func SetVersion(value string) {
	version = strings.TrimSpace(value)
	if version == "" {
		version = "dev"
	}
	tracepkg.SetVersion(version)
	uipkg.SetVersion(version)
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
		uipkg.PrintErrorTo(deps.Stderr, uipkg.Enabled(viewOptions(config{})), err.Error())
		return 2
	}

	effective := effectivePresentation(cfg, deps)
	ui := effective.ANSI

	switch cfg.CommandKind {
	case "config-init":
		if err := configpkg.InitConfigFileTo(deps.Stdout, ui); err != nil {
			uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
		return 0
	case "config-path":
		path, err := configpkg.SettingsPath()
		if err != nil {
			uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
		uipkg.RenderPanel(deps.Stdout, ui, "config", uipkg.ColorCyan, []string{path})
		return 0
	}

	appCtx := parentCtx
	stop := func() {}
	if !cfg.Interactive {
		// One-shot runs exit when Ctrl+C cancels the application context.
		appCtx, stop = signal.NotifyContext(parentCtx, os.Interrupt)
	}
	defer stop()

	ctxInfo, err := executorpkg.GetContext(appCtx, executorContextOptions(cfg))
	if err != nil {
		uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
		return 1
	}
	deps.Renderer = uipkg.NewRenderer(deps.Stdout, presentation{Style: effective.Style, ANSI: effective.ANSI, User: promptPresentationUser(cfg, ctxInfo.User)})

	traceCfg := traceOptions(cfg)
	trace, err := tracepkg.OpenSession(traceCfg, ctxInfo)
	if err != nil {
		uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
		return 1
	}
	deps.Trace = trace
	if trace != nil {
		trace.Record("session_start", "", "", -1, tracepkg.SessionStartData(traceCfg, ctxInfo))
		defer func() {
			trace.Record("session_end", "", "", -1, nil)
			_ = trace.Close()
		}()
	}

	if cfg.Interactive {
		if err := runInteractive(appCtx, deps, ui, cfg, &ctxInfo); err != nil {
			uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
			return 1
		}
		return 0
	}

	turn, err := runTurn(appCtx, deps, ui, turnRequest{
		Config:      cfg,
		ContextInfo: &ctxInfo,
		Instruction: cfg.Instruction,
	})
	if err != nil {
		switch {
		case errors.Is(err, core.ErrAborted), errors.Is(err, context.Canceled):
			uipkg.PrintErrorTo(deps.Stderr, ui, "execution aborted")
			return 130
		default:
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				code := exitErr.ExitCode()
				if code <= 0 {
					code = 1
				}
				uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
				return code
			}
			uipkg.PrintErrorTo(deps.Stderr, ui, err.Error())
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
		cfg := configpkg.DefaultConfig()
		configpkg.ApplyEnvConfig(&cfg)
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
	cfg := configpkg.DefaultConfig()
	fileCfg, configPath, err := configpkg.LoadFileConfig()
	if err != nil {
		return config{}, err
	}
	cfg.ConfigPath = configPath
	configpkg.ApplyFileConfig(&cfg, fileCfg)
	configpkg.ApplyEnvConfig(&cfg)
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
	configpkg.ApplyModelEnvOverrides(&cfg)
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
		configPath, _ := configpkg.SettingsPath()
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

// printModelProfilesTo lists configured model profiles and marks the active one.
func printModelProfilesTo(target io.Writer, ui bool, cfg config) {
	if len(cfg.Models) == 0 {
		uipkg.RenderPanel(target, ui, "models", uipkg.ColorYellow, []string{
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
	uipkg.RenderPanel(target, ui, "models", uipkg.ColorCyan, lines)
}

// printVisualThemesTo lists every selectable visual style and marks the configured one.
func printVisualThemesTo(target io.Writer, ui bool, cfg config) {
	lines := make([]string, 0, len(configpkg.VisualStyles()))
	for _, visualStyle := range configpkg.VisualStyles() {
		marker := " "
		if visualStyle == cfg.VisualStyle {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, visualStyle))
	}
	uipkg.RenderPanel(target, ui, "themes", uipkg.ColorCyan, lines)
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
	if err := configpkg.PersistDefaultModel(*cfg, selected.Name); err != nil {
		return fmt.Errorf("model switched to %s, but default_model was not persisted: %w", selected.Name, err)
	}
	return nil
}

// switchInteractiveTheme persists and applies one visual style between turns.
func switchInteractiveTheme(cfg *config, deps *runtimeDeps, user string, name string) error {
	selected := configpkg.NormalizeVisualStyle(name, "")
	if selected == "" {
		return fmt.Errorf("visual theme %q not found", strings.TrimSpace(name))
	}

	next := *cfg
	next.VisualStyle = selected
	if err := configpkg.PersistVisualStyle(next, selected); err != nil {
		return fmt.Errorf("theme was not changed: %w", err)
	}

	effective := effectivePresentation(next, *deps)
	nextRenderer := uipkg.NewRenderer(deps.Stdout, presentation{
		Style: effective.Style,
		ANSI:  effective.ANSI,
		User:  promptPresentationUser(next, user),
	})
	*cfg = next
	deps.Renderer = nextRenderer
	return nil
}
