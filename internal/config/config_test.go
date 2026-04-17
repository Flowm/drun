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
		{"missing-image", Preset{}, true},
		{"two-pms", Preset{Image: "alpine", Layer: map[string][]string{"apk": {"jq"}, "apt": {"jq"}}}, true},
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
	// Override a preset we expect to ship: pick any name from the embedded
	// set and replace it. To keep the test robust we add a brand-new name
	// and also override "shellcheck" if present.
	yaml := `
mytool:
  image: alpine
  entrypoint: echo
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
	if sc, ok := ps["shellcheck"]; ok {
		if sc.Image != "example.com/override:latest" {
			t.Errorf("shellcheck override not applied: %q", sc.Image)
		}
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
