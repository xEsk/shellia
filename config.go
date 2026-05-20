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
	CommandKind            string
	Instruction            string
	Interactive            bool
	BaseURL                string
	APIKey                 string
	Model                  string
	CommandTimeout         time.Duration
	RequestTimeout         time.Duration
	YesSafe                bool
	ContinueOnError        bool
	ConfirmationDefault    confirmationDefault
	CaptureStdoutBytes     int
	CaptureStderrBytes     int
	ObservationOutputChars int
	SummaryOutputChars     int
	MemoryObservationChars int
	MaxObservationEntries  int
	TruncationStrategy     truncationStrategy
	ShowSystemOutput       bool
	ShellMode              commandEngineMode
	CommandMode            commandEngineMode
	PlanOnly               bool
	AskConfirmPlan         bool
	AskConfirmPlanOnly     bool
	Debug                  bool
	RawResponse            bool
	NoColor                bool
	Verbose                bool
	ShowCommandPopup       bool
}

// fileConfig mirrors the structure of ~/.shellia/config.toml.
// Boolean fields are pointers so that absent keys leave the application default untouched.
type fileConfig struct {
	LLM struct {
		BaseURL string `toml:"base_url"`
		Model   string `toml:"model"`
		APIKey  string `toml:"api_key"`
	} `toml:"llm"`
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
	Output struct {
		CaptureStdoutBytes     int    `toml:"capture_stdout_bytes"`
		CaptureStderrBytes     int    `toml:"capture_stderr_bytes"`
		ObservationOutputChars int    `toml:"observation_output_chars"`
		SummaryOutputChars     int    `toml:"summary_output_chars"`
		MemoryObservationChars int    `toml:"memory_observation_chars"`
		MaxObservationEntries  int    `toml:"max_observation_entries"`
		TruncationStrategy     string `toml:"truncation_strategy"`
	} `toml:"output"`
	UI struct {
		Verbose          *bool `toml:"verbose"`
		NoColor          *bool `toml:"no_color"`
		ShowSystemOutput *bool `toml:"show_system_output"`
		ShowCommandPopup *bool `toml:"show_command_popup"`
	} `toml:"ui"`
}

// defaultConfig returns the built-in baseline values for Shellia.
func defaultConfig() config {
	return config{
		BaseURL:                defaultBaseURL,
		Model:                  defaultModel,
		CommandTimeout:         defaultTimeout,
		RequestTimeout:         60 * time.Second,
		ConfirmationDefault:    confirmationDefaultNone,
		CaptureStdoutBytes:     128 * 1024,
		CaptureStderrBytes:     256 * 1024,
		ObservationOutputChars: 1200,
		SummaryOutputChars:     4000,
		MemoryObservationChars: 400,
		MaxObservationEntries:  4,
		TruncationStrategy:     truncationMixed,
		ShowSystemOutput:       true,
		AskConfirmPlan:         true,
		AskConfirmPlanOnly:     true,
		ShellMode:              commandEngineInteractive,
		CommandMode:            commandEnginePlain,
		ShowCommandPopup:       true,
	}
}

// applyFileConfig merges the persistent file into the base config.
// Boolean fields are only applied when explicitly present in the file.
func applyFileConfig(cfg *config, fileCfg fileConfig) {
	if strings.TrimSpace(fileCfg.LLM.BaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(fileCfg.LLM.BaseURL)
	}
	if strings.TrimSpace(fileCfg.LLM.Model) != "" {
		cfg.Model = strings.TrimSpace(fileCfg.LLM.Model)
	}
	if strings.TrimSpace(fileCfg.LLM.APIKey) != "" {
		cfg.APIKey = strings.TrimSpace(fileCfg.LLM.APIKey)
	}
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
}

// applyEnvConfig applies environment variables on top of the persistent file.
// Priority: SHELLIA_* > OPENAI_* (compatibility fallback).
func applyEnvConfig(cfg *config) {
	cfg.BaseURL = getenvFallback(cfg.BaseURL, "SHELLIA_BASE_URL", "OPENAI_BASE_URL")
	cfg.Model = getenvFallback(cfg.Model, "SHELLIA_MODEL", "OPENAI_MODEL")
	if apiKey := getenvFallback("", "SHELLIA_API_KEY", "OPENAI_API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}
	cfg.ShellMode = normalizeCommandEngineMode(getenvFallback(string(cfg.ShellMode), "SHELLIA_SHELL_MODE"), cfg.ShellMode)
	cfg.CommandMode = normalizeCommandEngineMode(getenvFallback(string(cfg.CommandMode), "SHELLIA_COMMAND_MODE"), cfg.CommandMode)
}

// loadFileConfig loads ~/.shellia/config.toml if it exists.
func loadFileConfig() (fileConfig, error) {
	path, err := settingsPath()
	if err != nil {
		return fileConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, nil
		}
		return fileConfig{}, err
	}

	var cfg fileConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	return cfg, nil
}

// settingsPath returns the expected path of the Shellia persistent config file.
func settingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".shellia", "config.toml"), nil
}

// initConfigFile creates ~/.shellia/config.toml with an initial readable template.
func initConfigFile(ui bool) error {
	return initConfigFileTo(os.Stdout, ui)
}

// initConfigFileTo creates ~/.shellia/config.toml and reports the result on the provided target.
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
	return `# Shellia configuration — ~/.shellia/config.toml
# All values shown are the built-in defaults.
# Environment variables override this file: SHELLIA_API_KEY, SHELLIA_BASE_URL,
# SHELLIA_MODEL, OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL (in priority order).

[llm]
# OpenAI-compatible API endpoint. Works with OpenAI, Ollama, Groq, LM Studio, etc.
base_url = "https://api.openai.com/v1"

# Model name passed to the API. Use any model your endpoint supports.
model = "gpt-5.4-mini"

# API key. Leave empty to rely on the SHELLIA_API_KEY or OPENAI_API_KEY env var.
api_key = ""

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
