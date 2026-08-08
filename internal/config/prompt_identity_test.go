package config

import (
	"strings"
	"testing"
)

func TestPromptIdentityConfigControlsVisibleUserLabel(t *testing.T) {
	cfg := defaultConfig()
	if cfg.PromptIdentity != PromptIdentityUser {
		t.Fatalf("default prompt identity = %q, want %q", cfg.PromptIdentity, PromptIdentityUser)
	}

	fileCfg := FileConfig{}
	fileCfg.UI.PromptIdentity = " YOU "
	applyFileConfig(&cfg, fileCfg)
	if cfg.PromptIdentity != PromptIdentityYou {
		t.Fatalf("configured prompt identity = %q, want %q", cfg.PromptIdentity, PromptIdentityYou)
	}

	if !strings.Contains(defaultConfigTemplate(), `prompt_identity = "user"`) {
		t.Fatal("generated config lacks the prompt identity default")
	}
}

func TestUnknownPromptIdentityKeepsCurrentValue(t *testing.T) {
	cfg := defaultConfig()
	fileCfg := FileConfig{}
	fileCfg.UI.PromptIdentity = "machine"
	applyFileConfig(&cfg, fileCfg)

	if cfg.PromptIdentity != PromptIdentityUser {
		t.Fatalf("unknown prompt identity changed value to %q", cfg.PromptIdentity)
	}
}
