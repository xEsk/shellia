package main

import "testing"

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

// TestApplyFileConfigCanDisableSystemOutput checks the UI output visibility config flag.
func TestApplyFileConfigCanDisableSystemOutput(t *testing.T) {
	cfg := defaultConfig()
	show := false
	fileCfg := fileConfig{}
	fileCfg.UI.ShowSystemOutput = &show

	applyFileConfig(&cfg, fileCfg, nil)

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

	applyFileConfig(&cfg, fileCfg, nil)

	if cfg.ShowCommandPopup {
		t.Fatalf("ShowCommandPopup = true, want false")
	}
}

// TestApplyFileConfigCanSetConfirmationDefault checks the Enter confirmation shortcut config.
func TestApplyFileConfigCanSetConfirmationDefault(t *testing.T) {
	cfg := defaultConfig()
	fileCfg := fileConfig{}
	fileCfg.Execution.ConfirmationDefault = "yes"

	applyFileConfig(&cfg, fileCfg, nil)

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
