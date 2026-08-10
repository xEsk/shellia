package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFileConfigRejectsUnknownKeys checks configuration typos fail instead of silently defaulting.
func TestLoadFileConfigRejectsUnknownKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	path := filepath.Join(home, ".config", "shellia", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("[execution]\nyes_sfae = true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, err := loadFileConfig()
	if err == nil || !strings.Contains(err.Error(), "execution.yes_sfae") {
		t.Fatalf("loadFileConfig() error = %v, want unknown execution.yes_sfae key", err)
	}
}

// TestPersistDefaultModelReplacesConfigAtomically checks default model persistence swaps in a complete file.
func TestPersistDefaultModelReplacesConfigAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := "default_model = \"openai\"\n\n[[models]]\nname = \"openai\"\n"
	if err := os.WriteFile(path, []byte(input), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() before update error = %v", err)
	}

	cfg := defaultConfig()
	cfg.ConfigPath = path
	if err := persistDefaultModel(cfg, "local"); err != nil {
		t.Fatalf("persistDefaultModel() error = %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() after update error = %v", err)
	}
	if os.SameFile(before, after) {
		t.Fatal("persistDefaultModel() rewrote the existing inode instead of atomically replacing it")
	}
	if got := after.Mode().Perm(); got != 0o640 {
		t.Fatalf("permissions = %o, want 640", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "default_model = \"local\"") {
		t.Fatalf("config = %q, want local default model", data)
	}
}

// TestPersistVisualStylePreservesConfigSymlink checks atomic persistence keeps managed dotfile links intact.
func TestPersistVisualStylePreservesConfigSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed-config.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("[ui]\nstyle = \"plain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	cfg := defaultConfig()
	cfg.ConfigPath = link
	if err := persistVisualStyle(cfg, VisualStyleGuide); err != nil {
		t.Fatalf("persistVisualStyle() error = %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat() error = %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("persistVisualStyle() replaced the config symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "[ui]\nstyle = \"guide\"\n" {
		t.Fatalf("target config = %q, want guide style", got)
	}
}
