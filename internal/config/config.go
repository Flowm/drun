package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

//go:embed presets.yaml
var embeddedPresets []byte

// Preset describes how to run a container.
type Preset struct {
	Image      string              `yaml:"image"`
	Layer      map[string][]string `yaml:"x-drun-layer,omitempty"`
	Home       string              `yaml:"-"`
	Mounts     []string            `yaml:"volumes,omitempty"`
	Env        map[string]string   `yaml:"environment,omitempty"`
	Ports      []string            `yaml:"ports,omitempty"`
	Entrypoint string              `yaml:"entrypoint,omitempty"`
	Command    []string            `yaml:"command,omitempty"`
	User       string              `yaml:"user,omitempty"`
}

// Presets is the full keyed collection.
type Presets map[string]Preset

type composeFile struct {
	Services map[string]Preset `yaml:"services"`
}

// Load merges the embedded defaults with the user's config at
// ~/.config/drun/presets.yaml (full replacement on name collision).
func Load() (Presets, error) {
	out, err := loadComposePresets(embeddedPresets)
	if err != nil {
		return nil, fmt.Errorf("parse embedded presets: %w", err)
	}

	userPath, err := userConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve user config path: %w", err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("read %s: %w", userPath, err)
	}
	user, err := loadComposePresets(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", userPath, err)
	}
	for k, v := range user {
		out[k] = v
	}
	return out, nil
}

func loadComposePresets(data []byte) (Presets, error) {
	var doc composeFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Services) == 0 {
		return nil, fmt.Errorf("services is required")
	}
	out := make(Presets, len(doc.Services))
	for name, preset := range doc.Services {
		preset.normalize()
		out[name] = preset
	}
	return out, nil
}

func (p *Preset) normalize() {
	if len(p.Env) == 0 {
		return
	}
	home, ok := p.Env["HOME"]
	if !ok {
		return
	}
	p.Home = home
	delete(p.Env, "HOME")
	if len(p.Env) == 0 {
		p.Env = nil
	}
}

func userConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "drun", "presets.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "drun", "presets.yaml"), nil
}

// validPackageName restricts layer package names to a conservative set of
// characters common across apk/apt/dnf/npm (including scoped npm names like
// `@scope/pkg`). This prevents a malicious user-supplied presets file from
// smuggling shell metacharacters into the `RUN` lines generated in the
// Dockerfile.
var validPackageName = regexp.MustCompile(`^@?[A-Za-z0-9][A-Za-z0-9._@/+-]*$`)

// Validate checks a single preset for consistency.
func (p Preset) Validate(name string) error {
	if p.Image == "" {
		return fmt.Errorf("preset %q: image is required", name)
	}
	for pm, pkgs := range p.Layer {
		switch pm {
		case "apk", "apt", "dnf", "npm":
		default:
			return fmt.Errorf("preset %q: unsupported package manager %q", name, pm)
		}
		for _, pkg := range pkgs {
			if !validPackageName.MatchString(pkg) {
				return fmt.Errorf("preset %q: invalid %s package name %q", name, pm, pkg)
			}
		}
	}
	return nil
}
