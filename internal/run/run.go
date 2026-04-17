package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flowm/drun/internal/config"
)

// Options are fields that can be supplied from CLI to override or augment a preset.
type Options struct {
	ExtraMounts  []string
	ExtraEnv     map[string]string
	ExtraPorts   []string
	Entrypoint   string // overrides preset.Entrypoint if non-empty
	User         string // overrides preset.User if non-empty
	Home         string // overrides preset.Home if non-empty
	ExtraLayers  map[string][]string
	DockerSocket bool // OR'd with preset.DockerSocket
}

// Assemble builds the docker run command for a preset + options.
// image is the image ref to run (either preset.Image or a built layer tag).
// extraArgs are appended after the entrypoint.
func Assemble(p config.Preset, opts Options, image string, extraArgs []string) []string {
	args := []string{"run", "--rm"}

	if isTerminal(os.Stdin) {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}

	args = append(args, "-v", cwd()+":/cwd", "-w", "/cwd")

	user := firstNonEmpty(opts.User, p.User)
	if user == "" {
		args = append(args, "-u", hostUser())
	} else if user != "default" {
		args = append(args, "-u", user)
	}

	home := firstNonEmpty(opts.Home, p.Home)
	if home != "" {
		args = append(args, "-e", "HOME="+home)
	}

	for k, v := range p.Env {
		args = append(args, "-e", k+"="+v)
	}
	for k, v := range opts.ExtraEnv {
		args = append(args, "-e", k+"="+v)
	}

	for _, m := range append(append([]string{}, p.Mounts...), opts.ExtraMounts...) {
		args = append(args, "-v", expandMount(m))
	}

	for _, port := range append(append([]string{}, p.Ports...), opts.ExtraPorts...) {
		args = append(args, "-p", port)
	}

	if p.DockerSocket || opts.DockerSocket {
		args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock")
	}

	entrypoint := firstNonEmpty(opts.Entrypoint, p.Entrypoint)
	if entrypoint != "" {
		args = append(args, "--entrypoint", entrypoint)
	}

	args = append(args, image)
	args = append(args, p.Args...)
	args = append(args, extraArgs...)
	return args
}

// Exec runs docker with args, replacing the current process (best-effort).
func Exec(args []string) error {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, append([]string{"docker"}, args...), os.Environ())
}

// Print writes the docker command that would be executed.
func Print(args []string) {
	fmt.Println("docker " + strings.Join(quoteAll(args), " "))
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'$") {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			out[i] = a
		}
	}
	return out
}

func expandMount(m string) string {
	parts := strings.SplitN(m, ":", 2)
	parts[0] = expandTilde(parts[0])
	return strings.Join(parts, ":")
}

func expandTilde(p string) string {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func hostUser() string {
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
