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

// TestParseArgsSelectsOnlyConfiguredModel checks a single model profile is selected automatically.
func TestParseArgsSelectsOnlyConfiguredModel(t *testing.T) {
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
}

// TestParseArgsSelectsFirstConfiguredModel checks the first profile is the fallback default.
func TestParseArgsSelectsFirstConfiguredModel(t *testing.T) {
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

[[models]]
name = "flag"
base_url = "http://localhost:8082/v1"
model = "flag-model"
`)
	t.Setenv("SHELLIA_MODEL_NAME", "env")

	envCfg, err := parseArgs([]string{"run git status"})
	if err != nil {
		t.Fatalf("parseArgs(env) error = %v", err)
	}
	if envCfg.ModelName != "env" {
		t.Fatalf("env ModelName = %q, want env", envCfg.ModelName)
	}

	flagCfg, err := parseArgs([]string{"--model-name", "flag", "run git status"})
	if err != nil {
		t.Fatalf("parseArgs(flag) error = %v", err)
	}
	if flagCfg.ModelName != "flag" {
		t.Fatalf("flag ModelName = %q, want flag", flagCfg.ModelName)
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

// TestInitConfigFileCreatesPreferredConfigPath checks config init writes the recommended path.
func TestInitConfigFileCreatesPreferredConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	var output strings.Builder
	if err := initConfigFileTo(&output, false); err != nil {
		t.Fatalf("initConfigFileTo() error = %v", err)
	}

	path := filepath.Join(home, ".config", "shellia", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if !strings.Contains(output.String(), path) {
		t.Fatalf("output = %q, want preferred path", output.String())
	}
}

// TestUpdateDefaultModelTOMLReplacesExisting checks the top-level default_model is updated in place.
func TestUpdateDefaultModelTOMLReplacesExisting(t *testing.T) {
	input := "# config\n default_model = \"openai\"\n\n[[models]]\nname = \"mlx\"\n"
	got := updateDefaultModelTOML(input, "mlx")
	want := "# config\ndefault_model = \"mlx\"\n\n[[models]]\nname = \"mlx\"\n"
	if got != want {
		t.Fatalf("updateDefaultModelTOML() = %q, want %q", got, want)
	}
}

// TestUpdateDefaultModelTOMLInsertsBeforeFirstTable checks missing defaults are inserted without reordering tables.
func TestUpdateDefaultModelTOMLInsertsBeforeFirstTable(t *testing.T) {
	input := "# config\n\n[[models]]\nname = \"mlx\"\n"
	got := updateDefaultModelTOML(input, "mlx")
	want := "# config\n\ndefault_model = \"mlx\"\n[[models]]\nname = \"mlx\"\n"
	if got != want {
		t.Fatalf("updateDefaultModelTOML() = %q, want %q", got, want)
	}
}

// TestPersistDefaultModelKeepsConfigBody checks only default_model is persisted.
func TestPersistDefaultModelKeepsConfigBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "# config\n\n[[models]]\nname = \"mlx\"\nmodel = \"qwen\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := defaultConfig()
	cfg.ConfigPath = path
	if err := persistDefaultModel(cfg, "mlx"); err != nil {
		t.Fatalf("persistDefaultModel() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "default_model = \"mlx\"") || !strings.Contains(got, "[[models]]\nname = \"mlx\"") {
		t.Fatalf("persisted config = %q, want default and original model body", got)
	}
}

// TestSettingsPathUsesXDGConfigHome checks the preferred config path follows XDG_CONFIG_HOME.
func TestSettingsPathUsesXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	path, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath() error = %v", err)
	}
	want := filepath.Join(configHome, "shellia", "config.toml")
	if path != want {
		t.Fatalf("settingsPath() = %q, want %q", path, want)
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
