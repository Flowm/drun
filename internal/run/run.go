package run

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/flowm/drun/internal/config"
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

	entrypoint := firstNonEmpty(opts.Entrypoint, p.Entrypoint)
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
	return fmt.Sprintf("%s-%04x", name, randSuffix())
}

func randSuffix() uint16 {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return uint16(r.Intn(1 << 16))
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

// MissingHostDirs returns the list of host-side mount paths that do not yet
// exist on the filesystem. Extra mounts from CLI are included. Paths with
// leading ~ are expanded. Entries that look like file mounts (the last path
// segment contains a dot) are skipped so we don't mkdir over an intended
// file-bind target.
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
		// Heuristic: skip entries whose final segment looks like a filename
		// (contains a '.'), e.g. ~/.ssh/config is a directory but
		// ~/.gitconfig is a file. The common pattern in this tool is
		// directory mounts for state; file mounts are rare. To stay safe,
		// only skip if the basename starts with a dot AND has another dot
		// later (e.g. "foo.json") — bare "~/.ssh" stays in.
		base := filepath.Base(host)
		if looksLikeFile(base) {
			continue
		}
		missing = append(missing, host)
	}
	return missing
}

// looksLikeFile returns true for basenames that are almost certainly files
// rather than directories (e.g. "config.yaml", "id_rsa.pub"). A single dot
// at the very start (dotdir like ".ssh") does NOT count as a file marker.
func looksLikeFile(base string) bool {
	trimmed := strings.TrimPrefix(base, ".")
	return strings.Contains(trimmed, ".")
}

// EnsureHostDirs prompts to create missing host-side mount directories.
// On a TTY: list the paths and ask yes/no once. On decline, return an error.
// Off a TTY: auto-create silently. stdout/stdin are used for the prompt.
func EnsureHostDirs(missing []string, in io.Reader, out io.Writer) error {
	if len(missing) == 0 {
		return nil
	}
	if isTerminal(os.Stdin) {
		fmt.Fprintln(out, "drun: the following mount paths do not exist on the host:")
		for _, p := range missing {
			fmt.Fprintf(out, "  %s\n", p)
		}
		fmt.Fprint(out, "Create them? [Y/n] ")
		r := bufio.NewReader(in)
		line, _ := r.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "" && ans != "y" && ans != "yes" {
			return fmt.Errorf("aborted: missing mount paths not created")
		}
	}
	for _, p := range missing {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", p, err)
		}
	}
	return nil
}
