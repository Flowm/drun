package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed presets.yaml
var embeddedPresets []byte

// Preset describes how to run a container.
type Preset struct {
	Image        string              `yaml:"image"`
	Layer        map[string][]string `yaml:"layer,omitempty"`
	Home         string              `yaml:"home,omitempty"`
	Mounts       []string            `yaml:"mounts,omitempty"`
	Env          map[string]string   `yaml:"env,omitempty"`
	Ports        []string            `yaml:"ports,omitempty"`
	Entrypoint   string              `yaml:"entrypoint,omitempty"`
	Args         []string            `yaml:"args,omitempty"`
	DockerSocket bool                `yaml:"docker_socket,omitempty"`
	User         string              `yaml:"user,omitempty"`
}

// Presets is the full keyed collection.
type Presets map[string]Preset

// Load merges the embedded defaults with the user's config at
// ~/.config/drun/presets.yaml (full replacement on name collision).
func Load() (Presets, error) {
	out := Presets{}
	if err := yaml.Unmarshal(embeddedPresets, &out); err != nil {
		return nil, fmt.Errorf("parse embedded presets: %w", err)
	}

	userPath, err := userConfigPath()
	if err == nil {
		if data, err := os.ReadFile(userPath); err == nil {
			user := Presets{}
			if err := yaml.Unmarshal(data, &user); err != nil {
				return nil, fmt.Errorf("parse %s: %w", userPath, err)
			}
			for k, v := range user {
				out[k] = v
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", userPath, err)
		}
	}
	return out, nil
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

// Validate checks a single preset for consistency.
func (p Preset) Validate(name string) error {
	if p.Image == "" {
		return fmt.Errorf("preset %q: image is required", name)
	}
	if len(p.Layer) > 1 {
		return fmt.Errorf("preset %q: only one package manager allowed in layer", name)
	}
	for pm := range p.Layer {
		switch pm {
		case "apk", "apt", "dnf":
		default:
			return fmt.Errorf("preset %q: unsupported package manager %q", name, pm)
		}
	}
	return nil
}
