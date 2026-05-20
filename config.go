package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type commandEngineMode string
type confirmationDefault string
type truncationStrategy string

const (
	commandEnginePlain       commandEngineMode = "plain"
	commandEngineInteractive commandEngineMode = "interactive"

	confirmationDefaultNone        confirmationDefault = "none"
	confirmationDefaultYes         confirmationDefault = "yes"
	confirmationDefaultNo          confirmationDefault = "no"
	confirmationDefaultEdit        confirmationDefault = "edit"
	confirmationDefaultInteractive confirmationDefault = "interactive"

	truncationStart truncationStrategy = "start"
	truncationEnd   truncationStrategy = "end"
	truncationMixed truncationStrategy = "mixed"
)

type config struct {
	// Runtime options come from CLI arguments and are not persisted in config.toml.
	CommandKind string
	Instruction string
	Interactive bool
	PlanOnly    bool

	// LLM options identify the OpenAI-compatible endpoint used for planning and summaries.
	BaseURL                string
	APIKey                 string
	Model                  string
	ModelName              string
	DefaultModelName       string
	Models                 []modelConfig
	SupportsResponseFormat bool

	// Execution options control command timeouts, confirmation flow and shell execution mode.
	CommandTimeout      time.Duration
	RequestTimeout      time.Duration
	YesSafe             bool
	ContinueOnError     bool
	ConfirmationDefault confirmationDefault
	AskConfirmPlan      bool
	AskConfirmPlanOnly  bool
	ShellMode           commandEngineMode
	CommandMode         commandEngineMode

	// Output options limit captured command output before it is shown, summarized or reused.
	CaptureStdoutBytes     int
	CaptureStderrBytes     int
	ObservationOutputChars int
	SummaryOutputChars     int
	MemoryObservationChars int
	MaxObservationEntries  int
	TruncationStrategy     truncationStrategy

	// UI options control terminal rendering and optional debugging output.
	ShowSystemOutput bool
	Debug            bool
	RawPrompt        bool
	RawResponse      bool
	NoColor          bool
	Verbose          bool
	ShowCommandPopup bool

	// Context options control which local facts are sent to the model and shown by /context.
	IncludeGit                bool
	IncludeUser               bool
	IncludeOS                 bool
	IncludeShell              bool
	IncludeCWD                bool
	IncludeSessionMemory      bool
	IncludeRecentObservations bool
}

type modelConfig struct {
	Name                   string
	BaseURL                string
	Model                  string
	APIKey                 string
	APIKeyEnv              string
	SupportsResponseFormat bool
}

type fileModelConfig struct {
	Name                   string `toml:"name"`
	BaseURL                string `toml:"base_url"`
	Model                  string `toml:"model"`
	APIKey                 string `toml:"api_key"`
	APIKeyEnv              string `toml:"api_key_env"`
	SupportsResponseFormat *bool  `toml:"supports_response_format"`
}

// fileConfig mirrors the structure of the persistent Shellia config file.
// Boolean fields are pointers so that absent keys leave the application default untouched.
type fileConfig struct {
	DefaultModelName string            `toml:"default_model"`
	Models           []fileModelConfig `toml:"models"`

	// [execution]
	Execution struct {
		TimeoutSeconds        int    `toml:"timeout_seconds"`
		RequestTimeoutSeconds int    `toml:"request_timeout_seconds"`
		YesSafe               *bool  `toml:"yes_safe"`
		ContinueOnError       *bool  `toml:"continue_on_error"`
		AskConfirmPlan        *bool  `toml:"ask_confirm_plan"`
		AskConfirmPlanOnly    *bool  `toml:"ask_confirm_plan_only"`
		ConfirmationDefault   string `toml:"confirmation_default"`
		ShellMode             string `toml:"shell_mode"`
		CommandMode           string `toml:"command_mode"`
	} `toml:"execution"`

	// [output]
	Output struct {
		CaptureStdoutBytes     int    `toml:"capture_stdout_bytes"`
		CaptureStderrBytes     int    `toml:"capture_stderr_bytes"`
		ObservationOutputChars int    `toml:"observation_output_chars"`
		SummaryOutputChars     int    `toml:"summary_output_chars"`
		MemoryObservationChars int    `toml:"memory_observation_chars"`
		MaxObservationEntries  int    `toml:"max_observation_entries"`
		TruncationStrategy     string `toml:"truncation_strategy"`
	} `toml:"output"`

	// [ui]
	UI struct {
		Verbose          *bool `toml:"verbose"`
		NoColor          *bool `toml:"no_color"`
		ShowSystemOutput *bool `toml:"show_system_output"`
		ShowCommandPopup *bool `toml:"show_command_popup"`
	} `toml:"ui"`

	// [context]
	Context struct {
		IncludeGit                *bool `toml:"include_git"`
		IncludeUser               *bool `toml:"include_user"`
		IncludeOS                 *bool `toml:"include_os"`
		IncludeShell              *bool `toml:"include_shell"`
		IncludeCWD                *bool `toml:"include_cwd"`
		IncludeSessionMemory      *bool `toml:"include_session_memory"`
		IncludeRecentObservations *bool `toml:"include_recent_observations"`
	} `toml:"context"`
}

// defaultConfig returns the built-in baseline values for Shellia.
func defaultConfig() config {
	return config{
		// Model configuration.
		SupportsResponseFormat: true,

		// [execution]
		CommandTimeout:      defaultTimeout,
		RequestTimeout:      60 * time.Second,
		ConfirmationDefault: confirmationDefaultNone,
		AskConfirmPlan:      true,
		AskConfirmPlanOnly:  true,
		ShellMode:           commandEngineInteractive,
		CommandMode:         commandEnginePlain,

		// [output]
		CaptureStdoutBytes:     128 * 1024,
		CaptureStderrBytes:     256 * 1024,
		ObservationOutputChars: 1200,
		SummaryOutputChars:     4000,
		MemoryObservationChars: 400,
		MaxObservationEntries:  4,
		TruncationStrategy:     truncationMixed,

		// [ui]
		ShowSystemOutput: true,
		ShowCommandPopup: true,

		// [context]
		IncludeGit:                true,
		IncludeUser:               true,
		IncludeOS:                 true,
		IncludeShell:              true,
		IncludeCWD:                true,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
	}
}

// applyFileConfig merges the persistent file into the base config.
// Boolean fields are only applied when explicitly present in the file.
func applyFileConfig(cfg *config, fileCfg fileConfig) {
	cfg.DefaultModelName = strings.TrimSpace(fileCfg.DefaultModelName)
	cfg.Models = normalizeModelConfigs(fileCfg.Models)

	if fileCfg.Execution.TimeoutSeconds > 0 {
		cfg.CommandTimeout = time.Duration(fileCfg.Execution.TimeoutSeconds) * time.Second
	}
	if fileCfg.Execution.RequestTimeoutSeconds > 0 {
		cfg.RequestTimeout = time.Duration(fileCfg.Execution.RequestTimeoutSeconds) * time.Second
	}
	if fileCfg.Execution.YesSafe != nil {
		cfg.YesSafe = *fileCfg.Execution.YesSafe
	}
	if fileCfg.Execution.ContinueOnError != nil {
		cfg.ContinueOnError = *fileCfg.Execution.ContinueOnError
	}
	if fileCfg.Execution.AskConfirmPlan != nil {
		cfg.AskConfirmPlan = *fileCfg.Execution.AskConfirmPlan
	}
	if fileCfg.Execution.AskConfirmPlanOnly != nil {
		cfg.AskConfirmPlanOnly = *fileCfg.Execution.AskConfirmPlanOnly
	}
	if strings.TrimSpace(fileCfg.Execution.ConfirmationDefault) != "" {
		cfg.ConfirmationDefault = normalizeConfirmationDefault(fileCfg.Execution.ConfirmationDefault, cfg.ConfirmationDefault)
	}
	if strings.TrimSpace(fileCfg.Execution.ShellMode) != "" {
		cfg.ShellMode = normalizeCommandEngineMode(fileCfg.Execution.ShellMode, cfg.ShellMode)
	}
	if strings.TrimSpace(fileCfg.Execution.CommandMode) != "" {
		cfg.CommandMode = normalizeCommandEngineMode(fileCfg.Execution.CommandMode, cfg.CommandMode)
	}
	if fileCfg.Output.CaptureStdoutBytes > 0 {
		cfg.CaptureStdoutBytes = fileCfg.Output.CaptureStdoutBytes
	}
	if fileCfg.Output.CaptureStderrBytes > 0 {
		cfg.CaptureStderrBytes = fileCfg.Output.CaptureStderrBytes
	}
	if fileCfg.Output.ObservationOutputChars > 0 {
		cfg.ObservationOutputChars = fileCfg.Output.ObservationOutputChars
	}
	if fileCfg.Output.SummaryOutputChars > 0 {
		cfg.SummaryOutputChars = fileCfg.Output.SummaryOutputChars
	}
	if fileCfg.Output.MemoryObservationChars > 0 {
		cfg.MemoryObservationChars = fileCfg.Output.MemoryObservationChars
	}
	if fileCfg.Output.MaxObservationEntries > 0 {
		cfg.MaxObservationEntries = fileCfg.Output.MaxObservationEntries
	}
	if strings.TrimSpace(fileCfg.Output.TruncationStrategy) != "" {
		cfg.TruncationStrategy = normalizeTruncationStrategy(fileCfg.Output.TruncationStrategy, cfg.TruncationStrategy)
	}
	if fileCfg.UI.Verbose != nil {
		cfg.Verbose = *fileCfg.UI.Verbose
	}
	if fileCfg.UI.NoColor != nil {
		cfg.NoColor = *fileCfg.UI.NoColor
	}
	if fileCfg.UI.ShowSystemOutput != nil {
		cfg.ShowSystemOutput = *fileCfg.UI.ShowSystemOutput
	}
	if fileCfg.UI.ShowCommandPopup != nil {
		cfg.ShowCommandPopup = *fileCfg.UI.ShowCommandPopup
	}
	if fileCfg.Context.IncludeGit != nil {
		cfg.IncludeGit = *fileCfg.Context.IncludeGit
	}
	if fileCfg.Context.IncludeUser != nil {
		cfg.IncludeUser = *fileCfg.Context.IncludeUser
	}
	if fileCfg.Context.IncludeOS != nil {
		cfg.IncludeOS = *fileCfg.Context.IncludeOS
	}
	if fileCfg.Context.IncludeShell != nil {
		cfg.IncludeShell = *fileCfg.Context.IncludeShell
	}
	if fileCfg.Context.IncludeCWD != nil {
		cfg.IncludeCWD = *fileCfg.Context.IncludeCWD
	}
	if fileCfg.Context.IncludeSessionMemory != nil {
		cfg.IncludeSessionMemory = *fileCfg.Context.IncludeSessionMemory
	}
	if fileCfg.Context.IncludeRecentObservations != nil {
		cfg.IncludeRecentObservations = *fileCfg.Context.IncludeRecentObservations
	}
}

// applyEnvConfig applies environment variables that do not depend on the selected model profile.
func applyEnvConfig(cfg *config) {
	if modelName := strings.TrimSpace(os.Getenv("SHELLIA_MODEL_NAME")); modelName != "" {
		cfg.ModelName = modelName
	}
	cfg.ShellMode = normalizeCommandEngineMode(getenvFallback(string(cfg.ShellMode), "SHELLIA_SHELL_MODE"), cfg.ShellMode)
	cfg.CommandMode = normalizeCommandEngineMode(getenvFallback(string(cfg.CommandMode), "SHELLIA_COMMAND_MODE"), cfg.CommandMode)
}

// applyModelEnvOverrides applies one-shot model endpoint overrides from the environment.
// Priority: SHELLIA_* > OPENAI_* (compatibility fallback).
func applyModelEnvOverrides(cfg *config) {
	cfg.BaseURL = getenvFallback(cfg.BaseURL, "SHELLIA_BASE_URL", "OPENAI_BASE_URL")
	cfg.Model = getenvFallback(cfg.Model, "SHELLIA_MODEL", "OPENAI_MODEL")
	if apiKey := getenvFallback("", "SHELLIA_API_KEY", "OPENAI_API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}
}

// normalizeModelConfigs converts file model entries into runtime profiles.
func normalizeModelConfigs(fileModels []fileModelConfig) []modelConfig {
	models := make([]modelConfig, 0, len(fileModels))
	for _, fileModel := range fileModels {
		supportsResponseFormat := true
		if fileModel.SupportsResponseFormat != nil {
			supportsResponseFormat = *fileModel.SupportsResponseFormat
		}
		models = append(models, modelConfig{
			Name:                   strings.TrimSpace(fileModel.Name),
			BaseURL:                strings.TrimSpace(fileModel.BaseURL),
			Model:                  strings.TrimSpace(fileModel.Model),
			APIKey:                 strings.TrimSpace(fileModel.APIKey),
			APIKeyEnv:              strings.TrimSpace(fileModel.APIKeyEnv),
			SupportsResponseFormat: supportsResponseFormat,
		})
	}
	return models
}

// loadFileConfig loads the preferred config file, falling back to the legacy path if needed.
func loadFileConfig() (fileConfig, error) {
	path, err := settingsPath()
	if err != nil {
		return fileConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			legacyPath, legacyErr := legacySettingsPath()
			if legacyErr != nil {
				return fileConfig{}, legacyErr
			}
			data, err = os.ReadFile(legacyPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fileConfig{}, nil
				}
				return fileConfig{}, err
			}
			path = legacyPath
		} else {
			return fileConfig{}, err
		}
	}

	var cfg fileConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	return cfg, nil
}

// settingsPath returns the preferred path of the Shellia persistent config file.
func settingsPath() (string, error) {
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome != "" {
		return filepath.Join(configHome, "shellia", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "shellia", "config.toml"), nil
}

// legacySettingsPath returns the old Shellia config path used as a read-only fallback.
func legacySettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shellia", "config.toml"), nil
}

// initConfigFile creates the preferred config file with an initial readable template.
func initConfigFile(ui bool) error {
	return initConfigFileTo(os.Stdout, ui)
}

// initConfigFileTo creates the preferred config file and reports the result on the provided target.
func initConfigFileTo(target io.Writer, ui bool) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			renderPanel(target, ui, "config", colorYellow, []string{
				"Config already exists.",
				path,
			})
			return nil
		}
		return fmt.Errorf("cannot create config file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	if _, err := f.WriteString(defaultConfigTemplate()); err != nil {
		return fmt.Errorf("cannot write config file: %w", err)
	}

	renderPanel(target, ui, "config", colorGreen, []string{
		"Config created.",
		path,
	})
	return nil
}

// defaultConfigTemplate returns the base template for the persistent config.
func defaultConfigTemplate() string {
	return `# Shellia configuration — ~/.config/shellia/config.toml
# If XDG_CONFIG_HOME is set, Shellia uses $XDG_CONFIG_HOME/shellia/config.toml instead.
# Legacy fallback: Shellia can still read ~/.shellia/config.toml when this file does not exist.
# All values shown are the built-in defaults.
# Environment variables override the selected model profile: SHELLIA_API_KEY,
# SHELLIA_BASE_URL, SHELLIA_MODEL, OPENAI_API_KEY, OPENAI_BASE_URL,
# OPENAI_MODEL (in priority order). SHELLIA_MODEL_NAME selects a profile.

# Model profile used by default. If omitted, Shellia uses the first configured model.
default_model = "openai"

[[models]]
name = "openai"
base_url = "https://api.openai.com/v1"
model = "gpt-5.4-mini"
api_key_env = "SHELLIA_API_KEY"
# Most OpenAI-compatible endpoints support response_format. Defaults to true when omitted.
supports_response_format = true

[[models]]
name = "llama-cpp"
base_url = "http://localhost:8080/v1"
model = "unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:UD-Q4_K_XL"
api_key = ""
# llama.cpp supports response_format on /v1/chat/completions.
supports_response_format = true

[[models]]
name = "mlx"
base_url = "http://localhost:8080/v1"
model = "mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit"
api_key = ""
# MLX LM Server should use prompt-only JSON guidance.
supports_response_format = false

[execution]
# Maximum seconds a single shell command may run before it is killed.
timeout_seconds = 120

# Maximum seconds to wait for a single LLM API response before giving up.
request_timeout_seconds = 60

# Automatically confirm commands classified as "safe" without prompting.
yes_safe = false

# Keep executing remaining plan steps even when one command returns a non-zero exit code.
continue_on_error = false

# Show the full plan and ask "Execute this plan? [y/n]" before running any commands.
ask_confirm_plan = true

# Ask for confirmation after showing the plan when running in plan-only mode (--plan flag).
ask_confirm_plan_only = true

# What pressing Enter means in a confirmation prompt.
# none        — must type y or n explicitly (safest)
# yes         — Enter accepts the step
# no          — Enter skips the step
# edit        — Enter opens the command in $EDITOR before running
# interactive — Enter opens an interactive sub-shell for manual inspection
confirmation_default = "none"

# How /shell sessions are executed.
# interactive — full PTY session (preserves colours, readline, ncurses apps)
# plain       — direct capture without a PTY
shell_mode = "interactive"

# How one-shot !<cmd> commands are executed.
# plain       — inline capture, output shown in the step box
# interactive — allocates a PTY (useful for commands that need a terminal)
command_mode = "plain"

[output]
# Hard cap on stdout bytes captured from each command (128 KB).
# Bytes beyond this limit are shown in the terminal but not sent to the LLM.
capture_stdout_bytes = 131072

# Hard cap on stderr bytes captured from each command (256 KB).
capture_stderr_bytes = 262144

# Maximum characters of command output passed to the planning LLM as observations
# between planning rounds. Smaller values save tokens; larger values give the model
# more context for re-planning.
observation_output_chars = 1200

# Maximum characters of command output passed to the summarizer LLM for the final answer.
summary_output_chars = 4000

# Maximum characters kept per command output in session memory (used across follow-up turns).
memory_observation_chars = 400

# Maximum number of past command outputs stored in session memory.
# Oldest entries are dropped first when the limit is reached. Set to 0 to disable the cap.
max_observation_entries = 4

# Which part of the output to keep when it exceeds the character limits above.
# start — keep the beginning (good for table headers and structured output)
# end   — keep the end (good for build errors and log tails)
# mixed — keep 1/3 from the start and 2/3 from the end, joined with a gap marker
truncation_strategy = "mixed"

[ui]
# Print extra information such as resolved instructions and session state on each turn.
verbose = false

# Disable ANSI colour codes in all terminal output.
no_color = false

# Show command stdout/stderr inline inside the step execution boxes.
show_system_output = true

# Show the step execution box around each running command.
show_command_popup = true

[context]
# Control which local context is shared with the planning model and shown by /context.
# Keep cwd, os and shell enabled unless privacy is more important than command accuracy.
include_git = true
include_user = true
include_os = true
include_shell = true
include_cwd = true
include_session_memory = true
include_recent_observations = true
`
}

// getenvFallback returns the first non-empty value among the given env keys, or the fallback.
func getenvFallback(fallback string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

// normalizeTruncationStrategy validates the output truncation strategy.
func normalizeTruncationStrategy(value string, fallback truncationStrategy) truncationStrategy {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(truncationStart):
		return truncationStart
	case string(truncationEnd):
		return truncationEnd
	case string(truncationMixed):
		return truncationMixed
	default:
		return fallback
	}
}

// normalizeCommandEngineMode validates the configurable modes of the manual engine.
func normalizeCommandEngineMode(value string, fallback commandEngineMode) commandEngineMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(commandEnginePlain):
		return commandEnginePlain
	case string(commandEngineInteractive):
		return commandEngineInteractive
	default:
		return fallback
	}
}

// normalizeConfirmationDefault validates the Enter shortcut used in confirmation prompts.
func normalizeConfirmationDefault(value string, fallback confirmationDefault) confirmationDefault {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none", "null":
		return confirmationDefaultNone
	case "y", "yes":
		return confirmationDefaultYes
	case "n", "no":
		return confirmationDefaultNo
	case "e", "edit":
		return confirmationDefaultEdit
	case "i", "interactive":
		return confirmationDefaultInteractive
	default:
		return fallback
	}
}
