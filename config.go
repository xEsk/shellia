package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type commandEngineMode string

const (
	commandEnginePlain       commandEngineMode = "plain"
	commandEngineInteractive commandEngineMode = "interactive"
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
	CaptureStdoutBytes     int
	CaptureStderrBytes     int
	ObservationOutputChars int
	SummaryOutputChars     int
	ShellMode              commandEngineMode
	CommandMode            commandEngineMode
	Debug                  bool
	RawResponse            bool
	NoColor                bool
	Verbose                bool
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
		ShellMode             string `toml:"shell_mode"`
		CommandMode           string `toml:"command_mode"`
	} `toml:"execution"`
	Output struct {
		CaptureStdoutBytes     int `toml:"capture_stdout_bytes"`
		CaptureStderrBytes     int `toml:"capture_stderr_bytes"`
		ObservationOutputChars int `toml:"observation_output_chars"`
		SummaryOutputChars     int `toml:"summary_output_chars"`
	} `toml:"output"`
	UI struct {
		Verbose *bool `toml:"verbose"`
		NoColor *bool `toml:"no_color"`
	} `toml:"ui"`
}

// defaultConfig returns the built-in baseline values for Shellia.
func defaultConfig() config {
	return config{
		BaseURL:                defaultBaseURL,
		Model:                  defaultModel,
		CommandTimeout:         defaultTimeout,
		RequestTimeout:         60 * time.Second,
		CaptureStdoutBytes:     128 * 1024,
		CaptureStderrBytes:     256 * 1024,
		ObservationOutputChars: 1200,
		SummaryOutputChars:     4000,
		ShellMode:              commandEngineInteractive,
		CommandMode:            commandEnginePlain,
	}
}

// applyFileConfig merges the persistent file into the base config.
// Boolean fields are only applied when explicitly present in the file.
func applyFileConfig(cfg *config, fileCfg fileConfig, err error) {
	if err != nil {
		return
	}

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
	if fileCfg.UI.Verbose != nil {
		cfg.Verbose = *fileCfg.UI.Verbose
	}
	if fileCfg.UI.NoColor != nil {
		cfg.NoColor = *fileCfg.UI.NoColor
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
		return fileConfig{}, fmt.Errorf("invalid config file: %w", err)
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
	path, err := settingsPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		renderPanel(os.Stdout, ui, "config", colorYellow, []string{
			"Config already exists.",
			path,
		})
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot inspect config path: %w", err)
	}

	if err := os.WriteFile(path, []byte(defaultConfigTemplate()), 0o600); err != nil {
		return fmt.Errorf("cannot write config file: %w", err)
	}

	renderPanel(os.Stdout, ui, "config", colorGreen, []string{
		"Config created.",
		path,
	})
	return nil
}

// defaultConfigTemplate returns the base template for the persistent config.
func defaultConfigTemplate() string {
	return `[llm]
base_url = "https://api.openai.com/v1"
model    = "gpt-5.4-mini"
api_key  = ""

[execution]
timeout_seconds         = 120
request_timeout_seconds = 60
yes_safe                = false
continue_on_error       = false
shell_mode              = "interactive"
command_mode            = "plain"

[output]
capture_stdout_bytes     = 131072
capture_stderr_bytes     = 262144
observation_output_chars = 1200
summary_output_chars     = 4000

[ui]
verbose  = false
no_color = false
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
