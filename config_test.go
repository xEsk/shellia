package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultConfigShowsSystemOutput checks the visible-output default stays unchanged.
func TestDefaultConfigShowsSystemOutput(t *testing.T) {
	if !defaultConfig().ShowSystemOutput {
		t.Fatalf("defaultConfig().ShowSystemOutput = false, want true")
	}
	if !defaultConfig().ShowCommandPopup {
		t.Fatalf("defaultConfig().ShowCommandPopup = false, want true")
	}
	if defaultConfig().ConfirmationDefault != confirmationDefaultNone {
		t.Fatalf("defaultConfig().ConfirmationDefault = %q, want %q", defaultConfig().ConfirmationDefault, confirmationDefaultNone)
	}
}

// TestDefaultConfigIncludesLocalContext checks context sharing defaults preserve current behaviour.
func TestDefaultConfigIncludesLocalContext(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.IncludeGit || !cfg.IncludeUser || !cfg.IncludeOS || !cfg.IncludeShell || !cfg.IncludeCWD || !cfg.IncludeSessionMemory || !cfg.IncludeRecentObservations {
		t.Fatalf("defaultConfig() context flags = %#v, want all enabled", cfg)
	}
}

// TestApplyFileConfigCanDisableContextFields checks the context visibility config flags.
func TestApplyFileConfigCanDisableContextFields(t *testing.T) {
	cfg := defaultConfig()
	disabled := false
	fileCfg := fileConfig{}
	fileCfg.Context.IncludeGit = &disabled
	fileCfg.Context.IncludeUser = &disabled
	fileCfg.Context.IncludeOS = &disabled
	fileCfg.Context.IncludeShell = &disabled
	fileCfg.Context.IncludeCWD = &disabled
	fileCfg.Context.IncludeSessionMemory = &disabled
	fileCfg.Context.IncludeRecentObservations = &disabled

	applyFileConfig(&cfg, fileCfg)

	if cfg.IncludeGit || cfg.IncludeUser || cfg.IncludeOS || cfg.IncludeShell || cfg.IncludeCWD || cfg.IncludeSessionMemory || cfg.IncludeRecentObservations {
		t.Fatalf("context flags = %#v, want all disabled", cfg)
	}
}

// TestApplyFileConfigCanDisableSystemOutput checks the UI output visibility config flag.
func TestApplyFileConfigCanDisableSystemOutput(t *testing.T) {
	cfg := defaultConfig()
	show := false
	fileCfg := fileConfig{}
	fileCfg.UI.ShowSystemOutput = &show

	applyFileConfig(&cfg, fileCfg)

	if cfg.ShowSystemOutput {
		t.Fatalf("ShowSystemOutput = true, want false")
	}
}

// TestApplyFileConfigCanHideCommandPopup checks the command popup visibility config flag.
func TestApplyFileConfigCanHideCommandPopup(t *testing.T) {
	cfg := defaultConfig()
	show := false
	fileCfg := fileConfig{}
	fileCfg.UI.ShowCommandPopup = &show

	applyFileConfig(&cfg, fileCfg)

	if cfg.ShowCommandPopup {
		t.Fatalf("ShowCommandPopup = true, want false")
	}
}

// TestApplyFileConfigCanSetConfirmationDefault checks the Enter confirmation shortcut config.
func TestApplyFileConfigCanSetConfirmationDefault(t *testing.T) {
	cfg := defaultConfig()
	fileCfg := fileConfig{}
	fileCfg.Execution.ConfirmationDefault = "yes"

	applyFileConfig(&cfg, fileCfg)

	if cfg.ConfirmationDefault != confirmationDefaultYes {
		t.Fatalf("ConfirmationDefault = %q, want %q", cfg.ConfirmationDefault, confirmationDefaultYes)
	}
}

// TestNormalizeConfirmationDefaultAcceptsShortAliases checks common shorthand values.
func TestNormalizeConfirmationDefaultAcceptsShortAliases(t *testing.T) {
	tests := map[string]confirmationDefault{
		"y":           confirmationDefaultYes,
		"n":           confirmationDefaultNo,
		"e":           confirmationDefaultEdit,
		"i":           confirmationDefaultInteractive,
		"null":        confirmationDefaultNone,
		"unsupported": confirmationDefaultNo,
	}

	for input, want := range tests {
		if got := normalizeConfirmationDefault(input, confirmationDefaultNo); got != want {
			t.Fatalf("normalizeConfirmationDefault(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestParseArgsEnablesPlanOnlyFlags checks --plan and -p request planning without execution.
func TestParseArgsEnablesPlanOnlyFlags(t *testing.T) {
	for _, flagName := range []string{"--plan", "-p"} {
		t.Run(flagName, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("SHELLIA_API_KEY", "test-key")

			cfg, err := parseArgs([]string{flagName, "create a file"})
			if err != nil {
				t.Fatalf("parseArgs() error = %v", err)
			}

			if !cfg.PlanOnly {
				t.Fatalf("PlanOnly = false, want true")
			}
			if cfg.Instruction != "create a file" {
				t.Fatalf("Instruction = %q, want %q", cfg.Instruction, "create a file")
			}
		})
	}
}

// TestParseArgsEnablesRawPrompt checks --raw-prompt prints model prompts explicitly.
func TestParseArgsEnablesRawPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELLIA_API_KEY", "test-key")

	cfg, err := parseArgs([]string{"--raw-prompt", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if !cfg.RawPrompt {
		t.Fatalf("RawPrompt = false, want true")
	}
}

// TestParseArgsRequiresAPIKeyForRemoteEndpoints checks hosted endpoints still need credentials.
func TestParseArgsRequiresAPIKeyForRemoteEndpoints(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	_, err := parseArgs([]string{
		"--base-url", "https://api.openai.com/v1",
		"--model", "test-model",
		"run git status",
	})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want missing API key")
	}
	if !strings.Contains(err.Error(), "missing API key") {
		t.Fatalf("parseArgs() error = %q, want missing API key", err.Error())
	}
}

// TestParseArgsAllowsEmptyAPIKeyForLoopbackEndpoints checks local model servers need no fake key.
func TestParseArgsAllowsEmptyAPIKeyForLoopbackEndpoints(t *testing.T) {
	for _, baseURL := range []string{
		"http://localhost:8080/v1",
		"http://127.0.0.1:8080/v1",
		"http://[::1]:8080/v1",
	} {
		t.Run(baseURL, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("SHELLIA_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")

			cfg, err := parseArgs([]string{
				"--base-url", baseURL,
				"--model", "test-model",
				"run git status",
			})
			if err != nil {
				t.Fatalf("parseArgs() error = %v", err)
			}
			if cfg.APIKey != "" {
				t.Fatalf("APIKey = %q, want empty", cfg.APIKey)
			}
			if cfg.BaseURL != baseURL {
				t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, baseURL)
			}
		})
	}
}

// TestLoadBaseConfigRejectsInvalidConfig checks broken TOML is surfaced instead of ignored.
func TestLoadBaseConfigRejectsInvalidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("[llm]\nmodel =\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadBaseConfig()
	if err == nil {
		t.Fatalf("loadBaseConfig() error = nil, want invalid config error")
	}
	if !strings.Contains(err.Error(), "invalid config file") || !strings.Contains(err.Error(), path) {
		t.Fatalf("loadBaseConfig() error = %q, want invalid config path", err)
	}
}
