package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	sessionpkg "github.com/xEsk/shellia/internal/session"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// TestConsumerOptionsMapOwnedConfigFields checks each app boundary receives
// exactly the values it owns from the full application configuration.
func TestConsumerOptionsMapOwnedConfigFields(t *testing.T) {
	cfg := config{
		BaseURL:                   "https://llm.example/v1",
		APIKey:                    "top-secret",
		Model:                     "provider-model",
		ModelName:                 "primary",
		Models:                    []modelConfig{{Name: "primary", Model: "provider-model", APIKey: "model-secret", APIKeyEnv: "MODEL_SECRET", BaseURL: "https://model.example/v1"}},
		SupportsResponseFormat:    true,
		RequestParams:             map[string]any{"temperature": int64(1)},
		CommandTimeout:            17 * time.Second,
		RequestTimeout:            23 * time.Second,
		YesSafe:                   true,
		ContinueOnError:           true,
		ConfirmationDefault:       configpkg.ConfirmationDefaultEdit,
		AskConfirmPlan:            true,
		PlanningMaxRounds:         9,
		CaptureStdoutBytes:        111,
		CaptureStderrBytes:        222,
		ObservationOutputChars:    333,
		MemoryObservationChars:    444,
		MaxObservationEntries:     5,
		TruncationStrategy:        core.TruncationEnd,
		ShowSystemOutput:          true,
		NoColor:                   true,
		Verbose:                   true,
		ShowCommandPopup:          true,
		VisualStyle:               configpkg.VisualStyleCards,
		IncludeUser:               true,
		IncludeOS:                 true,
		IncludeShell:              true,
		IncludeCWD:                true,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
		TraceEnabled:              true,
		TraceDir:                  "/tmp/shellia-traces",
		Interactive:               true,
		PlanOnly:                  true,
	}

	if got, want := executorContextOptions(cfg), (executorpkg.ContextOptions{IncludeUser: true}); !reflect.DeepEqual(got, want) {
		t.Fatalf("executorContextOptions() = %#v, want %#v", got, want)
	}
	if got, want := executorOptions(cfg), (executorpkg.Options{
		CommandTimeout:      17 * time.Second,
		YesSafe:             true,
		ContinueOnError:     true,
		ConfirmationDefault: configpkg.ConfirmationDefaultEdit,
		CaptureStdoutBytes:  111,
		CaptureStderrBytes:  222,
		ShowSystemOutput:    true,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("executorOptions() = %#v, want %#v", got, want)
	}
	if got, want := viewOptions(cfg), (uipkg.ViewOptions{
		ModelName:        "primary",
		Model:            "provider-model",
		Models:           []uipkg.ModelOption{{Name: "primary", Model: "provider-model"}},
		Verbose:          true,
		AskConfirmPlan:   true,
		NoColor:          true,
		ShowCommandPopup: true,
		IncludeCWD:       true,
		IncludeUser:      true,
		IncludeOS:        true,
		IncludeShell:     true,
		VisualStyle:      configpkg.VisualStyleCards,
		PlanOnly:         true,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("viewOptions() = %#v, want %#v", got, want)
	}
	if got, want := llmClientOptions(cfg), (llmpkg.ClientOptions{
		BaseURL:                "https://llm.example/v1",
		APIKey:                 "top-secret",
		Model:                  "provider-model",
		RequestTimeout:         23 * time.Second,
		SupportsResponseFormat: true,
		RequestParams:          map[string]any{"temperature": int64(1)},
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("llmClientOptions() = %#v, want %#v", got, want)
	}
	if got, want := llmPromptOptions(cfg), (llmpkg.PromptOptions{
		PlanOnly:                  true,
		IncludeCWD:                true,
		IncludeOS:                 true,
		IncludeShell:              true,
		IncludeUser:               true,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
		MaxObservationEntries:     5,
		ObservationOutputChars:    333,
		TruncationStrategy:        core.TruncationEnd,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("llmPromptOptions() = %#v, want %#v", got, want)
	}
	if got, want := sessionMemoryOptions(cfg), (sessionpkg.MemoryOptions{
		MaxObservationEntries:  5,
		MemoryObservationChars: 444,
		TruncationStrategy:     core.TruncationEnd,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("sessionMemoryOptions() = %#v, want %#v", got, want)
	}
	if got, want := traceOptions(cfg), (tracepkg.Options{
		TraceEnabled:              true,
		TraceDir:                  "/tmp/shellia-traces",
		ModelName:                 "primary",
		Model:                     "provider-model",
		BaseURL:                   "https://llm.example/v1",
		Interactive:               true,
		PlanOnly:                  true,
		YesSafe:                   true,
		AskConfirmPlan:            true,
		PlanningMaxRounds:         9,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
		CaptureStdoutBytes:        111,
		CaptureStderrBytes:        222,
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("traceOptions() = %#v, want %#v", got, want)
	}
}

// TestNonLLMOptionsExcludeRequestParams checks arbitrary provider values stay outside unrelated consumers.
func TestNonLLMOptionsExcludeRequestParams(t *testing.T) {
	for _, value := range []any{
		uipkg.ViewOptions{},
		uipkg.ModelOption{},
		executorpkg.ContextOptions{},
		executorpkg.Options{},
		sessionpkg.MemoryOptions{},
		tracepkg.Options{},
	} {
		typeOf := reflect.TypeOf(value)
		if _, ok := typeOf.FieldByName("RequestParams"); ok {
			t.Fatalf("%s unexpectedly exposes RequestParams", typeOf)
		}
	}
}

// TestPresentationAndExecutionOptionsExcludeProviderSecrets checks provider
// credentials cannot cross into UI or executor option types.
func TestPresentationAndExecutionOptionsExcludeProviderSecrets(t *testing.T) {
	for _, value := range []any{
		uipkg.ViewOptions{},
		uipkg.ModelOption{},
		executorpkg.ContextOptions{},
		executorpkg.Options{},
	} {
		typeOf := reflect.TypeOf(value)
		for _, field := range []string{"APIKey", "APIKeyEnv", "BaseURL"} {
			if _, ok := typeOf.FieldByName(field); ok {
				t.Fatalf("%s unexpectedly exposes %s", typeOf, field)
			}
		}
	}
}

// TestDefaultConfigShowsSystemOutput checks the visible-output default stays unchanged.
func TestDefaultConfigShowsSystemOutput(t *testing.T) {
	if !configpkg.DefaultConfig().ShowSystemOutput {
		t.Fatalf("configpkg.DefaultConfig().ShowSystemOutput = false, want true")
	}
	if !configpkg.DefaultConfig().ShowCommandPopup {
		t.Fatalf("configpkg.DefaultConfig().ShowCommandPopup = false, want true")
	}
	if configpkg.DefaultConfig().ConfirmationDefault != configpkg.ConfirmationDefaultNone {
		t.Fatalf("configpkg.DefaultConfig().ConfirmationDefault = %q, want %q", configpkg.DefaultConfig().ConfirmationDefault, configpkg.ConfirmationDefaultNone)
	}
	if configpkg.DefaultConfig().PlanningMaxRounds != 5 {
		t.Fatalf("configpkg.DefaultConfig().PlanningMaxRounds = %d, want 5", configpkg.DefaultConfig().PlanningMaxRounds)
	}
	if configpkg.DefaultConfig().TraceEnabled {
		t.Fatalf("configpkg.DefaultConfig().TraceEnabled = true, want false")
	}
	if configpkg.DefaultConfig().TraceDir != "" {
		t.Fatalf("configpkg.DefaultConfig().TraceDir = %q, want empty", configpkg.DefaultConfig().TraceDir)
	}
}

// TestDefaultConfigUsesThreeThousandObservationCharacters checks the built-in
// prompt evidence budget is practical for typical CLI output.
func TestDefaultConfigUsesThreeThousandObservationCharacters(t *testing.T) {
	if got := configpkg.DefaultConfig().ObservationOutputChars; got != 3000 {
		t.Fatalf("configpkg.DefaultConfig().ObservationOutputChars = %d, want 3000", got)
	}
}

// TestLoadBaseConfigResolvesEveryVisualStyle checks the user-facing TOML
// contract at the same boundary used by application startup.
func TestLoadBaseConfigResolvesEveryVisualStyle(t *testing.T) {
	tests := []struct {
		name    string
		uiBlock string
		want    configpkg.VisualStyle
	}{
		{name: "plain", uiBlock: "[ui]\nstyle = \"plain\"", want: configpkg.VisualStylePlain},
		{name: "guide", uiBlock: "[ui]\nstyle = \"guide\"", want: configpkg.VisualStyleGuide},
		{name: "bands", uiBlock: "[ui]\nstyle = \"bands\"", want: configpkg.VisualStyleBands},
		{name: "cards", uiBlock: "[ui]\nstyle = \"cards\"", want: configpkg.VisualStyleCards},
		{name: "absent defaults to guide", uiBlock: "[ui]\nverbose = true", want: configpkg.VisualStyleGuide},
		{name: "unknown falls back to guide", uiBlock: "[ui]\nstyle = \"unknown\"", want: configpkg.VisualStyleGuide},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeShelliaConfig(t, tt.uiBlock)

			cfg, err := loadBaseConfig()
			if err != nil {
				t.Fatalf("loadBaseConfig() error = %v", err)
			}
			if cfg.VisualStyle != tt.want {
				t.Fatalf("VisualStyle = %q, want %q", cfg.VisualStyle, tt.want)
			}
		})
	}
}

// TestDefaultConfigIncludesLocalContext checks context sharing defaults preserve current behaviour.
func TestDefaultConfigIncludesLocalContext(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	if !cfg.IncludeUser || !cfg.IncludeOS || !cfg.IncludeShell || !cfg.IncludeCWD || !cfg.IncludeSessionMemory || !cfg.IncludeRecentObservations {
		t.Fatalf("configpkg.DefaultConfig() context flags = %#v, want all enabled", cfg)
	}
}

// TestDefaultConfigTemplateOmitsImplicitGitContext checks new config files do
// not advertise ambient Git repository detection.
func TestDefaultConfigTemplateOmitsImplicitGitContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := configpkg.InitConfigFileTo(&output, false); err != nil {
		t.Fatalf("configpkg.InitConfigFileTo() error = %v", err)
	}
	path := filepath.Join(home, ".config", "shellia", "config.toml")
	template, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if strings.Contains(string(template), "include_git") {
		t.Fatalf("generated config contains obsolete include_git setting: %q", template)
	}
}

// TestLoadFileConfigAcceptsObsoleteIncludeGit checks existing user configs keep
// loading after the ambient Git option is removed.
func TestLoadFileConfigAcceptsObsoleteIncludeGit(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("[context]\ninclude_git = true\ninclude_cwd = true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fileCfg, loadedPath, err := configpkg.LoadFileConfig()
	if err != nil {
		t.Fatalf("configpkg.LoadFileConfig() error = %v", err)
	}
	if loadedPath != path {
		t.Fatalf("configpkg.LoadFileConfig() path = %q, want %q", loadedPath, path)
	}
	if fileCfg.Context.IncludeCWD == nil || !*fileCfg.Context.IncludeCWD {
		t.Fatalf("configpkg.LoadFileConfig() did not preserve supported context setting: %#v", fileCfg.Context)
	}
}

// TestApplyFileConfigCanDisableContextFields checks the context visibility Config flags.
func TestApplyFileConfigCanDisableContextFields(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	disabled := false
	fileCfg := configpkg.FileConfig{}
	fileCfg.Context.IncludeUser = &disabled
	fileCfg.Context.IncludeOS = &disabled
	fileCfg.Context.IncludeShell = &disabled
	fileCfg.Context.IncludeCWD = &disabled
	fileCfg.Context.IncludeSessionMemory = &disabled
	fileCfg.Context.IncludeRecentObservations = &disabled

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if cfg.IncludeUser || cfg.IncludeOS || cfg.IncludeShell || cfg.IncludeCWD || cfg.IncludeSessionMemory || cfg.IncludeRecentObservations {
		t.Fatalf("context flags = %#v, want all disabled", cfg)
	}
}

// TestApplyFileConfigCanDisableSystemOutput checks the UI output visibility Config flag.
func TestApplyFileConfigCanDisableSystemOutput(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	fileCfg := configpkg.FileConfig{}
	fileCfg.UI.ShowSystemOutput = new(false)

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if cfg.ShowSystemOutput {
		t.Fatalf("ShowSystemOutput = true, want false")
	}
}

// TestApplyFileConfigCanHideCommandPopup checks the command popup visibility Config flag.
func TestApplyFileConfigCanHideCommandPopup(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	fileCfg := configpkg.FileConfig{}
	fileCfg.UI.ShowCommandPopup = new(false)

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if cfg.ShowCommandPopup {
		t.Fatalf("ShowCommandPopup = true, want false")
	}
}

// TestApplyFileConfigCanSetConfirmationDefault checks the Enter confirmation shortcut Config.
func TestApplyFileConfigCanSetConfirmationDefault(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	fileCfg := configpkg.FileConfig{}
	fileCfg.Execution.ConfirmationDefault = "yes"

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if cfg.ConfirmationDefault != configpkg.ConfirmationDefaultYes {
		t.Fatalf("configpkg.ConfirmationDefault = %q, want %q", cfg.ConfirmationDefault, configpkg.ConfirmationDefaultYes)
	}
}

// TestApplyFileConfigCanSetPlanningMaxRounds checks the planning round cap is configurable.
func TestApplyFileConfigCanSetPlanningMaxRounds(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	fileCfg := configpkg.FileConfig{}
	fileCfg.Execution.PlanningMaxRounds = 7

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if cfg.PlanningMaxRounds != 7 {
		t.Fatalf("PlanningMaxRounds = %d, want 7", cfg.PlanningMaxRounds)
	}
}

// TestApplyFileConfigCanEnableTrace checks the trace Config block.
func TestApplyFileConfigCanEnableTrace(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	fileCfg := configpkg.FileConfig{}
	fileCfg.Trace.Enabled = new(bool)
	*fileCfg.Trace.Enabled = true
	fileCfg.Trace.Dir = "/tmp/shellia-traces"

	configpkg.ApplyFileConfig(&cfg, fileCfg)

	if !cfg.TraceEnabled {
		t.Fatalf("TraceEnabled = false, want true")
	}
	if cfg.TraceDir != "/tmp/shellia-traces" {
		t.Fatalf("TraceDir = %q, want /tmp/shellia-traces", cfg.TraceDir)
	}
}

// TestApplyEnvConfigCanOverridePlanningMaxRounds checks one-shot planning cap overrides.
func TestApplyEnvConfigCanOverridePlanningMaxRounds(t *testing.T) {
	t.Setenv("SHELLIA_PLANNING_MAX_ROUNDS", "9")
	cfg := configpkg.DefaultConfig()

	configpkg.ApplyEnvConfig(&cfg)

	if cfg.PlanningMaxRounds != 9 {
		t.Fatalf("PlanningMaxRounds = %d, want 9", cfg.PlanningMaxRounds)
	}
}

// TestNormalizeConfirmationDefaultAcceptsShortAliases checks common shorthand values.
func TestNormalizeConfirmationDefaultAcceptsShortAliases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  configpkg.ConfirmationDefault
	}{
		{name: "yes alias", input: "y", want: configpkg.ConfirmationDefaultYes},
		{name: "no alias", input: "n", want: configpkg.ConfirmationDefaultNo},
		{name: "edit alias", input: "e", want: configpkg.ConfirmationDefaultEdit},
		{name: "interactive alias", input: "i", want: configpkg.ConfirmationDefaultInteractive},
		{name: "null alias", input: "null", want: configpkg.ConfirmationDefaultNone},
		{name: "unsupported fallback", input: "unsupported", want: configpkg.ConfirmationDefaultNo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configpkg.NormalizeConfirmationDefault(tt.input, configpkg.ConfirmationDefaultNo); got != tt.want {
				t.Fatalf("configpkg.NormalizeConfirmationDefault(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseArgsEnablesPlanOnlyFlags checks --plan and -p request planning without execution.
func TestParseArgsEnablesPlanOnlyFlags(t *testing.T) {
	for _, flagName := range []string{"--plan", "-p"} {
		t.Run(flagName, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("SHELLIA_BASE_URL", "http://localhost:8080/v1")
			t.Setenv("SHELLIA_MODEL", "test-model")

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
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", "http://localhost:8080/v1")
	t.Setenv("SHELLIA_MODEL", "test-model")

	cfg, err := parseArgs([]string{"--raw-prompt", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if !cfg.RawPrompt {
		t.Fatalf("RawPrompt = false, want true")
	}
}

// TestParseArgsEnablesTrace checks the one-shot trace flag.
func TestParseArgsEnablesTrace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", "http://localhost:8080/v1")
	t.Setenv("SHELLIA_MODEL", "test-model")

	cfg, err := parseArgs([]string{"--trace", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if !cfg.TraceEnabled {
		t.Fatalf("TraceEnabled = false, want true")
	}
}

// TestParseArgsCanDisableconfiguredTrace checks CLI flags override Config defaults.
func TestParseArgsCanDisableconfiguredTrace(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"

[trace]
enabled = true
`)

	cfg, err := parseArgs([]string{"--trace=false", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.TraceEnabled {
		t.Fatalf("TraceEnabled = true, want false")
	}
}

// TestParseArgsTraceDirOverridesConfig checks the one-shot trace directory flag.
func TestParseArgsTraceDirOverridesConfig(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"

[trace]
enabled = true
dir = "/tmp/Config-traces"
`)

	cfg, err := parseArgs([]string{"--trace-dir", "/tmp/flag-traces", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.TraceDir != "/tmp/flag-traces" {
		t.Fatalf("TraceDir = %q, want /tmp/flag-traces", cfg.TraceDir)
	}
}

// TestParseArgsRejectsInvalidConfigCommand checks config commands fail before planning.
func TestParseArgsRejectsInvalidConfigCommand(t *testing.T) {
	for _, args := range [][]string{
		{"config"},
		{"config", "unknown"},
		{"config", "path", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := parseArgs(args)
			if err == nil {
				t.Fatalf("parseArgs() error = nil, want invalid config command")
			}
			if !strings.Contains(err.Error(), "invalid config command") {
				t.Fatalf("parseArgs() error = %q, want invalid config command", err.Error())
			}
		})
	}
}

// TestParseArgsRejectsNonPositiveTimeouts checks timeout flags cannot disable safeguards.
func TestParseArgsRejectsNonPositiveTimeouts(t *testing.T) {
	for _, args := range [][]string{
		{"--timeout", "0", "run git status"},
		{"--timeout", "-1", "run git status"},
		{"--request-timeout", "0", "run git status"},
		{"--request-timeout", "-1", "run git status"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("SHELLIA_BASE_URL", "http://localhost:8080/v1")
			t.Setenv("SHELLIA_MODEL", "test-model")

			_, err := parseArgs(args)
			if err == nil {
				t.Fatalf("parseArgs() error = nil, want timeout validation error")
			}
			if !strings.Contains(err.Error(), "must be greater than 0") {
				t.Fatalf("parseArgs() error = %q, want positive timeout validation", err.Error())
			}
		})
	}
}

// TestParseArgsSelectsOnlyconfiguredModel checks a single model profile is selected automatically.
func TestParseArgsSelectsOnlyconfiguredModel(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"
`)

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ModelName != "local" || cfg.BaseURL != "http://localhost:8080/v1" || cfg.Model != "local-model" {
		t.Fatalf("selected model = (%q, %q, %q), want local profile", cfg.ModelName, cfg.BaseURL, cfg.Model)
	}
	if !cfg.SupportsResponseFormat {
		t.Fatalf("SupportsResponseFormat = false, want true by default")
	}
	if cfg.SupportsJSONSchema {
		t.Fatalf("SupportsJSONSchema = true, want false by default")
	}
}

// TestParseArgsSelectsFirstconfiguredModel checks the first profile is the fallback default.
func TestParseArgsSelectsFirstconfiguredModel(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "first"
base_url = "http://localhost:8080/v1"
model = "first-model"

[[models]]
name = "second"
base_url = "http://localhost:8081/v1"
model = "second-model"
`)

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ModelName != "first" || cfg.Model != "first-model" {
		t.Fatalf("selected model = (%q, %q), want first profile", cfg.ModelName, cfg.Model)
	}
}

// TestParseArgsSelectsDefaultModel checks default_model selects a named profile.
func TestParseArgsSelectsDefaultModel(t *testing.T) {
	writeShelliaConfig(t, `
default_model = "second"

[[models]]
name = "first"
base_url = "http://localhost:8080/v1"
model = "first-model"

[[models]]
name = "second"
base_url = "http://localhost:8081/v1"
model = "second-model"
supports_response_format = false
`)

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ModelName != "second" || cfg.Model != "second-model" {
		t.Fatalf("selected model = (%q, %q), want second profile", cfg.ModelName, cfg.Model)
	}
	if cfg.SupportsResponseFormat {
		t.Fatalf("SupportsResponseFormat = true, want configured false")
	}
}

// TestParseArgsSelectsModelRequestParams checks provider body fields follow the selected profile into LLM options.
func TestParseArgsSelectsModelRequestParams(t *testing.T) {
	writeShelliaConfig(t, `
default_model = "custom"

[[models]]
name = "provider-default"
base_url = "http://localhost:8080/v1"
model = "default-model"

[[models]]
name = "custom"
base_url = "http://localhost:8081/v1"
model = "custom-model"

[models.request_params]
temperature = 1
thinking = { type = "enabled" }
`)

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	want := map[string]any{
		"temperature": int64(1),
		"thinking":    map[string]any{"type": "enabled"},
	}
	if !reflect.DeepEqual(cfg.RequestParams, want) {
		t.Fatalf("cfg.RequestParams = %#v, want %#v", cfg.RequestParams, want)
	}
	if got := llmClientOptions(cfg).RequestParams; !reflect.DeepEqual(got, want) {
		t.Fatalf("llm RequestParams = %#v, want %#v", got, want)
	}
	if got := cfg.Models[0].RequestParams; len(got) != 0 {
		t.Fatalf("provider-default RequestParams = %#v, want empty", got)
	}
}

// TestParseArgsRejectsProtectedRequestParamsInInactiveProfile checks every reachable profile is validated at startup.
func TestParseArgsRejectsProtectedRequestParamsInInactiveProfile(t *testing.T) {
	writeShelliaConfig(t, `
default_model = "valid"

[[models]]
name = "valid"
base_url = "http://localhost:8080/v1"
model = "valid-model"

[[models]]
name = "invalid"
base_url = "http://localhost:8081/v1"
model = "invalid-model"

[models.request_params]
model = "override"
`)

	_, err := parseArgs([]string{"run git status"})
	if err == nil {
		t.Fatal("parseArgs() error = nil, want inactive profile validation error")
	}
	if !strings.Contains(err.Error(), `configured model profile "invalid"`) || !strings.Contains(err.Error(), "request_params.model") {
		t.Fatalf("parseArgs() error = %q, want profile and protected path", err.Error())
	}
	if strings.Contains(err.Error(), "override") {
		t.Fatalf("parseArgs() error = %q, want no configured value", err.Error())
	}
}

// TestParseArgsRejectsNonJSONModelRequestParams checks TOML values fail before reaching the provider boundary.
func TestParseArgsRejectsNonJSONModelRequestParams(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		wantPath string
	}{
		{name: "datetime", param: "created_at = 2026-08-11T08:00:00Z", wantPath: "request_params.created_at"},
		{name: "nan", param: "temperature = nan", wantPath: "request_params.temperature"},
		{name: "infinity", param: "temperature = +inf", wantPath: "request_params.temperature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeShelliaConfig(t, `
[[models]]
name = "invalid"
base_url = "http://localhost:8080/v1"
model = "invalid-model"

[models.request_params]
`+tt.param+"\n")

			_, err := parseArgs([]string{"run git status"})
			if err == nil || !strings.Contains(err.Error(), `configured model profile "invalid"`) || !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("parseArgs() error = %v, want profile and path %q", err, tt.wantPath)
			}
		})
	}
}

// TestParseArgsModelNamePrecedence checks flag and env profile selection precedence.
func TestParseArgsModelNamePrecedence(t *testing.T) {
	writeShelliaConfig(t, `
default_model = "first"

[[models]]
name = "first"
base_url = "http://localhost:8080/v1"
model = "first-model"

[[models]]
name = "env"
base_url = "http://localhost:8081/v1"
model = "env-model"

[models.request_params]
profile_marker = "env"

[[models]]
name = "flag"
base_url = "http://localhost:8082/v1"
model = "flag-model"

[models.request_params]
profile_marker = "flag"
`)
	t.Setenv("SHELLIA_MODEL_NAME", "env")

	envCfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs(env) error = %v", err)
	}
	if envCfg.ModelName != "env" || envCfg.RequestParams["profile_marker"] != "env" {
		t.Fatalf("env profile = (%q, %#v), want env params", envCfg.ModelName, envCfg.RequestParams)
	}

	flagCfg, err := parseArgs([]string{"--model-name", "flag", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs(flag) error = %v", err)
	}
	if flagCfg.ModelName != "flag" || flagCfg.RequestParams["profile_marker"] != "flag" {
		t.Fatalf("flag profile = (%q, %#v), want flag params", flagCfg.ModelName, flagCfg.RequestParams)
	}
}

// TestParseArgsModelProfileAPIKeyEnv checks api_key_env resolves profile credentials.
func TestParseArgsModelProfileAPIKeyEnv(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "remote"
base_url = "https://api.example.test/v1"
model = "remote-model"
api_key_env = "TEST_REMOTE_KEY"
`)
	t.Setenv("TEST_REMOTE_KEY", "secret-key")

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.APIKey != "secret-key" {
		t.Fatalf("APIKey = %q, want env key", cfg.APIKey)
	}
}

// TestParseArgsModelOverrides checks endpoint flags override the selected profile.
func TestParseArgsModelOverrides(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"

[models.request_params]
temperature = 0.25
`)

	cfg, err := parseArgs([]string{
		"--base-url", "http://localhost:9090/v1",
		"--model", "override-model",
		"--api-key", "override-key",
		"run git status",
	})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.BaseURL != "http://localhost:9090/v1" || cfg.Model != "override-model" || cfg.APIKey != "override-key" {
		t.Fatalf("overrides = (%q, %q, %q), want flag values", cfg.BaseURL, cfg.Model, cfg.APIKey)
	}
	if cfg.RequestParams["temperature"] != 0.25 {
		t.Fatalf("RequestParams = %#v, want selected profile params preserved", cfg.RequestParams)
	}
}

// TestParseArgsAcceptsUnboundedObservationOutputBudget checks positive user
// budgets are not rejected by an application-defined upper limit.
func TestParseArgsAcceptsUnboundedObservationOutputBudget(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"

[output]
observation_output_chars = 250000
`)

	cfg, err := parseArgs([]string{"inspect status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ObservationOutputChars != 250000 {
		t.Fatalf("ObservationOutputChars = %d, want 250000", cfg.ObservationOutputChars)
	}
}

// TestParseArgsErrorsWithoutModelConfiguration checks a model endpoint is required.
func TestParseArgsErrorsWithoutModelConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", "")
	t.Setenv("SHELLIA_MODEL", "")

	_, err := parseArgs([]string{"run git status"})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want missing model configuration")
	}
	if !strings.Contains(err.Error(), "missing model configuration") {
		t.Fatalf("parseArgs() error = %q, want missing model configuration", err.Error())
	}
}

// TestParseArgsErrorsForUnknownModelProfile checks unknown profile selection fails clearly.
func TestParseArgsErrorsForUnknownModelProfile(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "local"
base_url = "http://localhost:8080/v1"
model = "local-model"
`)

	_, err := parseArgs([]string{"--model-name", "missing", "run git status"})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want missing profile error")
	}
	if !strings.Contains(err.Error(), `configured model profile "missing" not found`) {
		t.Fatalf("parseArgs() error = %q, want missing profile", err.Error())
	}
}

// TestParseArgsErrorsForIncompleteModelProfile checks incomplete profiles fail clearly.
func TestParseArgsErrorsForIncompleteModelProfile(t *testing.T) {
	writeShelliaConfig(t, `
[[models]]
name = "broken"
base_url = "http://localhost:8080/v1"
`)

	_, err := parseArgs([]string{"run git status"})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want incomplete profile error")
	}
	if !strings.Contains(err.Error(), `configured model profile "broken" is missing model`) {
		t.Fatalf("parseArgs() error = %q, want missing model", err.Error())
	}
}

// TestParseArgsRequiresAPIKeyForRemoteEndpoints checks hosted endpoints still need credentials.
func TestParseArgsRequiresAPIKeyForRemoteEndpoints(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	_, err := parseArgs([]string{
		"--base-url", "https://api.openai.com/v1",
		"--model", "test-model",
		"run git status",
	})
	if err == nil {
		t.Fatalf("parseArgs() error = nil, want missing api key")
	}
	if !strings.Contains(err.Error(), "missing api key") {
		t.Fatalf("parseArgs() error = %q, want missing api key", err.Error())
	}
}

// TestParseArgsRejectsRemoteHTTP checks insecure hosted endpoints fail during configuration.
func TestParseArgsRejectsRemoteHTTP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	_, err := parseArgs([]string{
		"--base-url", "http://api.example.invalid/v1",
		"--api-key", "audit-secret",
		"--model", "test-model",
		"run git status",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "https") {
		t.Fatalf("parseArgs() error = %v, want remote HTTPS requirement", err)
	}
}

// TestParseArgsRejectsInvalidBaseURLs checks malformed or unsupported endpoint URLs fail early.
func TestParseArgsRejectsInvalidBaseURLs(t *testing.T) {
	baseURLs := []string{
		"api.example.invalid/v1",
		"ftp://api.example.invalid/v1",
		"https:///v1",
	}
	for _, baseURL := range baseURLs {
		t.Run(baseURL, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("SHELLIA_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")

			_, err := parseArgs([]string{
				"--base-url", baseURL,
				"--api-key", "audit-secret",
				"--model", "test-model",
				"run git status",
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "base url") {
				t.Fatalf("parseArgs() error = %v, want invalid base URL", err)
			}
		})
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
			t.Setenv("XDG_CONFIG_HOME", "")
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
	t.Setenv("XDG_CONFIG_HOME", "")

	path := filepath.Join(home, ".config", "shellia", "config.toml")
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

// TestLoadBaseConfigUsesLegacyConfigFallback checks the old config path is still read when the XDG path is absent.
func TestLoadBaseConfigUsesLegacyConfigFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_MODEL_NAME", "")
	t.Setenv("SHELLIA_BASE_URL", "")
	t.Setenv("SHELLIA_MODEL", "")
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")

	path := filepath.Join(home, ".shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`
[[models]]
name = "legacy"
base_url = "http://localhost:8080/v1"
model = "legacy-model"
api_key = ""
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ModelName != "legacy" {
		t.Fatalf("ModelName = %q, want legacy", cfg.ModelName)
	}
}

// TestLoadBaseConfigPrefersXDGConfig checks the recommended path wins over the legacy fallback.
func TestLoadBaseConfigPrefersXDGConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_MODEL_NAME", "")
	t.Setenv("SHELLIA_BASE_URL", "")
	t.Setenv("SHELLIA_MODEL", "")
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")

	legacyPath := filepath.Join(home, ".shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(legacy) error = %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`
[[models]]
name = "legacy"
base_url = "http://localhost:8080/v1"
model = "legacy-model"
api_key = ""
`), 0o600); err != nil {
		t.Fatalf("WriteFile(legacy) error = %v", err)
	}

	preferredPath := filepath.Join(home, ".config", "shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(preferredPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(preferred) error = %v", err)
	}
	if err := os.WriteFile(preferredPath, []byte(`
[[models]]
name = "preferred"
base_url = "http://localhost:8081/v1"
model = "preferred-model"
api_key = ""
`), 0o600); err != nil {
		t.Fatalf("WriteFile(preferred) error = %v", err)
	}

	cfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs() error = %v", err)
	}
	if cfg.ModelName != "preferred" {
		t.Fatalf("ModelName = %q, want preferred", cfg.ModelName)
	}
}

// TestInitConfigFileCreatesPreferredConfigPath checks Config init writes the recommended path.
func TestInitConfigFileCreatesPreferredConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := configpkg.InitConfigFileTo(&output, false); err != nil {
		t.Fatalf("configpkg.InitConfigFileTo() error = %v", err)
	}

	path := filepath.Join(home, ".config", "shellia", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if !strings.Contains(output.String(), path) {
		t.Fatalf("output = %q, want preferred path", output.String())
	}
}

// TestInitConfigFileUsesGPT56LunaByDefault checks new configurations select the intended OpenAI model.
func TestInitConfigFileUsesGPT56LunaByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := configpkg.InitConfigFileTo(&output, false); err != nil {
		t.Fatalf("configpkg.InitConfigFileTo() error = %v", err)
	}

	fileCfg, _, err := configpkg.LoadFileConfig()
	if err != nil {
		t.Fatalf("configpkg.LoadFileConfig() error = %v", err)
	}
	if fileCfg.DefaultModelName != "openai" {
		t.Fatalf("default_model = %q, want openai", fileCfg.DefaultModelName)
	}
	if len(fileCfg.Models) == 0 || fileCfg.Models[0].Model != "gpt-5.6-luna" {
		t.Fatalf("first model profile = %#v, want gpt-5.6-luna", fileCfg.Models)
	}
	if len(fileCfg.Models[0].RequestParams) != 0 {
		t.Fatalf("default request_params = %#v, want provider defaults", fileCfg.Models[0].RequestParams)
	}
}

// TestInitConfigFileUsesFivePlanningRoundsByDefault checks new configurations preserve the built-in planning cap.
func TestInitConfigFileUsesFivePlanningRoundsByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := configpkg.InitConfigFileTo(&output, false); err != nil {
		t.Fatalf("configpkg.InitConfigFileTo() error = %v", err)
	}

	fileCfg, _, err := configpkg.LoadFileConfig()
	if err != nil {
		t.Fatalf("configpkg.LoadFileConfig() error = %v", err)
	}
	if fileCfg.Execution.PlanningMaxRounds != 5 {
		t.Fatalf("planning_max_rounds = %d, want 5", fileCfg.Execution.PlanningMaxRounds)
	}
}

// TestInitConfigFileUsesThreeThousandObservationCharactersByDefault checks
// generated configurations match the built-in prompt evidence budget.
func TestInitConfigFileUsesThreeThousandObservationCharactersByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := configpkg.InitConfigFileTo(&output, false); err != nil {
		t.Fatalf("configpkg.InitConfigFileTo() error = %v", err)
	}

	fileCfg, _, err := configpkg.LoadFileConfig()
	if err != nil {
		t.Fatalf("configpkg.LoadFileConfig() error = %v", err)
	}
	if fileCfg.Output.ObservationOutputChars != 3000 {
		t.Fatalf("observation_output_chars = %d, want 3000", fileCfg.Output.ObservationOutputChars)
	}
}

// TestUpdateDefaultModelTOMLReplacesExisting checks the top-level default_model is updated in place.
func TestUpdateDefaultModelTOMLReplacesExisting(t *testing.T) {
	input := "# Config\n default_model = \"openai\"\n\n[[models]]\nname = \"mlx\"\n"
	got := configpkg.UpdateDefaultModelTOML(input, "mlx")
	want := "# Config\ndefault_model = \"mlx\"\n\n[[models]]\nname = \"mlx\"\n"
	if got != want {
		t.Fatalf("configpkg.UpdateDefaultModelTOML() = %q, want %q", got, want)
	}
}

// TestUpdateDefaultModelTOMLInsertsBeforeFirstTable checks missing defaults are inserted without reordering tables.
func TestUpdateDefaultModelTOMLInsertsBeforeFirstTable(t *testing.T) {
	input := "# Config\n\n[[models]]\nname = \"mlx\"\n"
	got := configpkg.UpdateDefaultModelTOML(input, "mlx")
	want := "# Config\n\ndefault_model = \"mlx\"\n[[models]]\nname = \"mlx\"\n"
	if got != want {
		t.Fatalf("configpkg.UpdateDefaultModelTOML() = %q, want %q", got, want)
	}
}

// TestPersistDefaultModelKeepsConfigBody checks only default_model is persisted.
func TestPersistDefaultModelKeepsConfigBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "# Config\n\n[[models]]\nname = \"mlx\"\nmodel = \"qwen\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := configpkg.DefaultConfig()
	cfg.ConfigPath = path
	if err := configpkg.PersistDefaultModel(cfg, "mlx"); err != nil {
		t.Fatalf("configpkg.PersistDefaultModel() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "default_model = \"mlx\"") || !strings.Contains(got, "[[models]]\nname = \"mlx\"") {
		t.Fatalf("persisted Config = %q, want default and original model body", got)
	}
}

// TestSettingsPathUsesXDGConfigHome checks the preferred config path follows XDG_CONFIG_HOME.
func TestSettingsPathUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	path, err := configpkg.SettingsPath()
	if err != nil {
		t.Fatalf("configpkg.SettingsPath() error = %v", err)
	}
	want := filepath.Join(configHome, "shellia", "config.toml")
	if path != want {
		t.Fatalf("configpkg.SettingsPath() = %q, want %q", path, want)
	}
}

func writeShelliaConfig(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_MODEL_NAME", "")
	t.Setenv("SHELLIA_BASE_URL", "")
	t.Setenv("SHELLIA_MODEL", "")
	t.Setenv("SHELLIA_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")

	path := filepath.Join(home, ".config", "shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
