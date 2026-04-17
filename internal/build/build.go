package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flowm/drun/internal/config"
)

// ImageNamePrefix is the local image namespace used for built layer images.
const ImageNamePrefix = "drun"

// NeedsBuild reports whether the preset declares any layer.
func NeedsBuild(p config.Preset) bool {
	for _, pkgs := range p.Layer {
		if len(pkgs) > 0 {
			return true
		}
	}
	return false
}

// Tag computes the deterministic local image tag for a preset.
func Tag(name string, p config.Preset) string {
	return fmt.Sprintf("%s/%s:%s", ImageNamePrefix, name, hash(p))
}

func hash(p config.Preset) string {
	var b strings.Builder
	b.WriteString(p.Image)
	b.WriteString("\n")
	b.WriteString(p.Home)
	b.WriteString("\n")

	pms := make([]string, 0, len(p.Layer))
	for pm := range p.Layer {
		pms = append(pms, pm)
	}
	sort.Strings(pms)
	for _, pm := range pms {
		pkgs := append([]string(nil), p.Layer[pm]...)
		sort.Strings(pkgs)
		b.WriteString(pm)
		b.WriteString(":")
		b.WriteString(strings.Join(pkgs, ","))
		b.WriteString("\n")
	}

	// Include parent dirs of mount targets under Home so changes to mount
	// layout invalidate the cached layer image.
	parents := mountParentDirs(p)
	for _, d := range parents {
		b.WriteString("mp:")
		b.WriteString(d)
		b.WriteString("\n")
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

// Dockerfile renders the Dockerfile contents for a preset.
func Dockerfile(p config.Preset) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", p.Image)
	b.WriteString("USER 0:0\n")

	for _, pm := range sortedKeys(p.Layer) {
		pkgs := p.Layer[pm]
		if len(pkgs) == 0 {
			continue
		}
		b.WriteString("RUN ")
		b.WriteString(installCmd(pm, pkgs))
		b.WriteString("\n")
	}

	if p.Home != "" {
		// Pre-create $HOME plus every parent directory of each bind-mount
		// target under $HOME, with mode 0777 so any uid selected at `docker
		// run` time (via -u) can write there. Without this, Docker's daemon
		// auto-creates missing bind-mount parents as root:root 0755, leaving
		// the unprivileged container user unable to write siblings under
		// e.g. /home/user/.config or /home/user/.cache.
		dirs := append([]string{p.Home}, mountParentDirs(p)...)
		fmt.Fprintf(&b, "RUN mkdir -p %s && chmod 777 %s\n",
			strings.Join(dirs, " "), strings.Join(dirs, " "))
	}
	return b.String()
}

// mountParentDirs returns the sorted, de-duplicated set of container-side
// parent directories of every mount target that lives under p.Home. The Home
// dir itself is excluded (callers add it separately). Only directory-style
// mounts contribute; file mounts like "~/.gitconfig:/home/user/.gitconfig:ro"
// contribute their containing directory as well, since that directory must
// exist for the file bind.
func mountParentDirs(p config.Preset) []string {
	if p.Home == "" {
		return nil
	}
	home := strings.TrimRight(p.Home, "/")
	seen := map[string]bool{}
	var out []string
	for _, m := range p.Mounts {
		// Mount spec: host:container[:opts]. Split off host, then take
		// the container-side path.
		_, rest, ok := strings.Cut(m, ":")
		if !ok {
			continue
		}
		ctr, _, _ := strings.Cut(rest, ":")
		if ctr == "" {
			continue
		}
		parent := filepath.Dir(ctr)
		// Walk up to (but not including) Home, collecting every
		// intermediate directory.
		for parent != "." && parent != "/" && parent != home {
			if strings.HasPrefix(parent, home+"/") {
				if !seen[parent] {
					seen[parent] = true
					out = append(out, parent)
				}
			} else {
				break
			}
			parent = filepath.Dir(parent)
		}
	}
	sort.Strings(out)
	return out
}

func installCmd(pm string, pkgs []string) string {
	joined := strings.Join(pkgs, " ")
	switch pm {
	case "apk":
		return "apk add --no-cache " + joined
	case "apt":
		return "apt-get update && apt-get install -y --no-install-recommends " + joined +
			" && rm -rf /var/lib/apt/lists/*"
	case "dnf":
		return "dnf install -y " + joined + " && dnf clean all"
	}
	return "false"
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ImageExists returns true if the local docker daemon has the given tag.
func ImageExists(tag string) bool {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// EnsureImage builds the layer image if not present (or if force is true).
// Returns the tag to run.
func EnsureImage(name string, p config.Preset, force bool) (string, error) {
	tag := Tag(name, p)
	if !force && ImageExists(tag) {
		return tag, nil
	}
	dir, err := os.MkdirTemp("", "drun-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	dfPath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte(Dockerfile(p)), 0o644); err != nil {
		return "", err
	}

	cmd := exec.Command("docker", "build", "-t", tag, dir)
	cmd.Stdout = os.Stderr // build output to stderr so it doesn't pollute pipes
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build %s: %w", tag, err)
	}
	return tag, nil
}

// PrintBuild emits what EnsureImage would do without running it.
func PrintBuild(name string, p config.Preset) {
	tag := Tag(name, p)
	fmt.Fprintf(os.Stdout, "# would build %s\n", tag)
	fmt.Fprintln(os.Stdout, "# Dockerfile:")
	for _, line := range strings.Split(strings.TrimRight(Dockerfile(p), "\n"), "\n") {
		fmt.Fprintf(os.Stdout, "#   %s\n", line)
	}
}
