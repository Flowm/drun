//go:build !windows

// Package run assembles and executes the `docker run` command for a preset.
package run

import (
	"bufio"
	"cmp"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/flowm/drun/internal/config"
	"golang.org/x/term"
)

// Options are fields that can be supplied from CLI to override or augment a preset.
type Options struct {
	ExtraMounts []string
	ExtraEnv    map[string]string
	ExtraPorts  []string
	Entrypoint  string // overrides preset.Entrypoint if non-empty
	User        string // overrides preset.User if non-empty
	Home        string // overrides preset.Home if non-empty
	ExtraLayers map[string][]string
}

var invalidContainerNameChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

// Assemble builds the docker run command for a preset + options.
// name is used for the container name.
// image is the image ref to run (either preset.Image or a built layer tag).
// extraArgs are appended after the entrypoint.
func Assemble(name string, p config.Preset, opts Options, image string, extraArgs []string) []string {
	args := []string{"run", "--rm"}
	args = append(args, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	args = append(args, "--name", uniqueContainerName(name))

	if isTerminal(os.Stdin) {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}

	args = append(args, "-v", cwd()+":/cwd", "-w", "/cwd")

	user := cmp.Or(opts.User, p.User)
	if user == "" {
		args = append(args, "-u", hostUser())
	} else if user != "default" {
		args = append(args, "-u", user)
	}

	home := cmp.Or(opts.Home, p.Home)
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

	entrypoint := cmp.Or(opts.Entrypoint, p.Entrypoint)
	if entrypoint != "" {
		args = append(args, "--entrypoint", entrypoint)
	}

	args = append(args, image)
	args = append(args, p.Command...)
	args = append(args, extraArgs...)
	return args
}

func uniqueContainerName(name string) string {
	name = strings.ToLower(name)
	name = invalidContainerNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_.")
	if name == "" {
		name = "drun"
	}
	return fmt.Sprintf("%s-%08x", name, randSuffix())
}

func randSuffix() uint32 {
	var buf [4]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		// crypto/rand should never fail; fall back to a non-constant value
		return uint32(os.Getpid())
	}
	return binary.BigEndian.Uint32(buf[:])
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
		out[i] = shellQuote(a)
	}
	return out
}

// shellQuote returns a POSIX shell-safe single-quoted form of s. The output
// is always quoted, even for "safe" strings, so that the emitted command is
// unambiguous when pasted into a shell regardless of extension (aliases,
// functions, etc.).
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// MissingHostDirs returns the host-side mount paths that do not yet exist.
// Extra mounts from CLI are included. Paths with leading ~ are expanded.
func MissingHostDirs(p config.Preset, opts Options) []string {
	all := append([]string{}, p.Mounts...)
	all = append(all, opts.ExtraMounts...)

	seen := map[string]bool{}
	var missing []string
	for _, m := range all {
		host, _, _ := strings.Cut(m, ":")
		host = expandTilde(host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		if _, err := os.Stat(host); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue // permission error etc. — leave it to docker to complain
		}
		missing = append(missing, host)
	}
	return missing
}

// EnsureHostDirs prompts to create missing host-side mount directories.
// Without a TTY it skips host-side creation and leaves missing paths alone.
func EnsureHostDirs(missing []string, in io.Reader, out io.Writer) error {
	return ensureHostDirs(missing, in, out, isTerminal(os.Stdin))
}

func ensureHostDirs(missing []string, in io.Reader, out io.Writer, interactive bool) error {
	if len(missing) == 0 {
		return nil
	}
	if !interactive {
		return nil
	}
	fmt.Fprintln(out, "drun: the following mount paths do not exist on the host:")
	for _, p := range missing {
		fmt.Fprintf(out, "  %s\n", p)
	}
	fmt.Fprint(out, "Create directories? [Y/n] ")
	r := bufio.NewReader(in)
	line, _ := r.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "" && ans != "y" && ans != "yes" {
		return fmt.Errorf("aborted: missing mount paths not created")
	}
	for _, p := range missing {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	return nil
}
