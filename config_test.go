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
