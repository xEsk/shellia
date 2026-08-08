package config

import (
	"io"

	"shellia/internal/core"
)

// DefaultConfig returns the built-in baseline values for Shellia.
func DefaultConfig() Config {
	return defaultConfig()
}

// ApplyFileConfig merges the persistent file into the base config.
func ApplyFileConfig(cfg *Config, fileCfg FileConfig) {
	applyFileConfig(cfg, fileCfg)
}

// ApplyEnvConfig applies environment variables that do not depend on the selected model profile.
func ApplyEnvConfig(cfg *Config) {
	applyEnvConfig(cfg)
}

// ApplyModelEnvOverrides applies one-shot model endpoint overrides from the environment.
func ApplyModelEnvOverrides(cfg *Config) {
	applyModelEnvOverrides(cfg)
}

// LoadFileConfig loads the preferred config file, falling back to the legacy path if needed.
func LoadFileConfig() (FileConfig, string, error) {
	return loadFileConfig()
}

// SettingsPath returns the preferred path of the Shellia persistent config file.
func SettingsPath() (string, error) {
	return settingsPath()
}

// InitConfigFileTo creates the preferred config file and reports the result on the provided target.
func InitConfigFileTo(target io.Writer, ui bool) error {
	return initConfigFileTo(target, ui)
}

// PersistDefaultModel writes the selected profile as the persistent default model.
func PersistDefaultModel(cfg Config, name string) error {
	return persistDefaultModel(cfg, name)
}

// VisualStyles returns every selectable terminal visual style in menu order.
func VisualStyles() []VisualStyle {
	return visualStyles()
}

// PersistVisualStyle writes the selected terminal visual style to [ui].style.
func PersistVisualStyle(cfg Config, style VisualStyle) error {
	return persistVisualStyle(cfg, style)
}

// UpdateDefaultModelTOML updates only the top-level default_model assignment.
func UpdateDefaultModelTOML(content string, name string) string {
	return updateDefaultModelTOML(content, name)
}

// NormalizeTruncationStrategy validates the output truncation strategy.
func NormalizeTruncationStrategy(value string, fallback core.TruncationStrategy) core.TruncationStrategy {
	return normalizeTruncationStrategy(value, fallback)
}

// NormalizeCommandEngineMode validates the configurable modes of the manual engine.
func NormalizeCommandEngineMode(value string, fallback CommandEngineMode) CommandEngineMode {
	return normalizeCommandEngineMode(value, fallback)
}

// NormalizeConfirmationDefault validates the Enter shortcut used in confirmation prompts.
func NormalizeConfirmationDefault(value string, fallback ConfirmationDefault) ConfirmationDefault {
	return normalizeConfirmationDefault(value, fallback)
}

// NormalizeVisualStyle validates the configurable terminal visual style.
func NormalizeVisualStyle(value string, fallback VisualStyle) VisualStyle {
	return normalizeVisualStyle(value, fallback)
}
