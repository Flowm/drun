package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       Preset
		wantErr bool
	}{
		{"ok-minimal", Preset{Image: "alpine"}, false},
		{"ok-with-apk", Preset{Image: "alpine", Layer: map[string][]string{"apk": {"jq"}}}, false},
		{"ok-with-npm", Preset{Image: "node", Layer: map[string][]string{"npm": {"@openai/codex"}}}, false},
		{"ok-with-mixed-layer", Preset{Image: "node", Layer: map[string][]string{"apk": {"git"}, "npm": {"@openai/codex"}}}, false},
		{"missing-image", Preset{}, true},
		{"unknown-pm", Preset{Image: "alpine", Layer: map[string][]string{"pacman": {"jq"}}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate(tc.name)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestLoadEmbedded(t *testing.T) {
	// Isolate from the developer's real config by pointing XDG_CONFIG_HOME
	// at an empty temp dir.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ps, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ps) == 0 {
		t.Fatal("expected at least one embedded preset")
	}
	for name, p := range ps {
		if err := p.Validate(name); err != nil {
			t.Errorf("embedded preset %q invalid: %v", name, err)
		}
	}
}

func TestLoadUserOverride(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "drun")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Override a preset we expect to ship and add a new one using the
	// compose-style schema.
	yaml := `
services:
  mytool:
    image: alpine
    entrypoint: echo
    command: [hello]
    environment:
      HOME: /tmp/home
      GREETING: hi
  shellcheck:
    image: example.com/override:latest
`
	if err := os.WriteFile(filepath.Join(cfgDir, "presets.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	ps, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := ps["mytool"].Image; got != "alpine" {
		t.Errorf("mytool.image = %q, want alpine", got)
	}
	if got := ps["mytool"].Entrypoint; got != "echo" {
		t.Errorf("mytool.entrypoint = %q, want echo", got)
	}
	if got := ps["mytool"].Command; len(got) != 1 || got[0] != "hello" {
		t.Errorf("mytool.command = %v, want [hello]", got)
	}
	if got := ps["mytool"].Home; got != "/tmp/home" {
		t.Errorf("mytool.home = %q, want /tmp/home", got)
	}
	if got := ps["mytool"].Env["GREETING"]; got != "hi" {
		t.Errorf("mytool.env[GREETING] = %q, want hi", got)
	}
	if _, ok := ps["mytool"].Env["HOME"]; ok {
		t.Error("mytool.env should not retain HOME after normalization")
	}
	if sc, ok := ps["shellcheck"]; ok {
		if sc.Image != "example.com/override:latest" {
			t.Errorf("shellcheck override not applied: %q", sc.Image)
		}
	}
}

func TestLoadUserMissingServices(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "drun")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "presets.yaml"), []byte("jq:\n  image: alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing services key")
	}
}

func TestLoadInvalidUserYAML(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "drun")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "presets.yaml"), []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed user YAML")
	}
}
