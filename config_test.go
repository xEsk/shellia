package main

import "testing"

// TestDefaultConfigShowsSystemOutput checks the visible-output default stays unchanged.
func TestDefaultConfigShowsSystemOutput(t *testing.T) {
	if defaultConfig().HideSystemOutput {
		t.Fatalf("defaultConfig().HideSystemOutput = true, want false")
	}
}

// TestApplyFileConfigCanHideSystemOutput checks the output visibility config flag.
func TestApplyFileConfigCanHideSystemOutput(t *testing.T) {
	cfg := defaultConfig()
	hide := true
	fileCfg := fileConfig{}
	fileCfg.Output.HideSystemOutput = &hide

	applyFileConfig(&cfg, fileCfg, nil)

	if !cfg.HideSystemOutput {
		t.Fatalf("HideSystemOutput = false, want true")
	}
}
