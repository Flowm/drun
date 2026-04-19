//go:build !windows

package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flowm/drun/internal/config"
)

// joinArgs makes substring checks cleaner while still asserting sequence.
func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

// contains checks that the sequence `want` appears consecutively in args.
func containsSeq(t *testing.T, args []string, want ...string) {
	t.Helper()
	s := joinArgs(args)
	w := joinArgs(want)
	if !strings.Contains(s, w) {
		t.Errorf("expected sequence %q in args %q", w, s)
	}
}

func TestAssembleDefaults(t *testing.T) {
	p := config.Preset{Image: "alpine"}
	args := Assemble("alpine", p, Options{}, "alpine", nil)

	if args[0] != "run" || args[1] != "--rm" {
		t.Errorf("expected run --rm prefix, got %v", args[:2])
	}
	containsSeq(t, args, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	if !containsSeqString(args, "--name", "alpine-") {
		t.Errorf("expected generated drun name, got %v", args)
	}
	// TTY flag depends on stdin; just assert one of them is present.
	if !(contains(args, "-it") || contains(args, "-i")) {
		t.Error("missing -it/-i")
	}
	containsSeq(t, args, "-w", "/cwd")
	// last arg is image
	if args[len(args)-1] != "alpine" {
		t.Errorf("last arg = %q, want alpine", args[len(args)-1])
	}
	// -u is auto-populated with host uid:gid
	if !contains(args, "-u") {
		t.Error("missing -u for default user")
	}
}

func TestAssembleUserDefault(t *testing.T) {
	// user: default means omit -u entirely
	p := config.Preset{Image: "alpine", User: "default"}
	args := Assemble("alpine", p, Options{}, "alpine", nil)
	if contains(args, "-u") {
		t.Errorf("user:default should omit -u, got %v", args)
	}
}

func TestAssembleUserExplicit(t *testing.T) {
	p := config.Preset{Image: "alpine", User: "1234:5678"}
	args := Assemble("alpine", p, Options{}, "alpine", nil)
	containsSeq(t, args, "-u", "1234:5678")
}

func TestAssembleHomeAndEnv(t *testing.T) {
	p := config.Preset{
		Image: "alpine",
		Home:  "/home/user",
		Env:   map[string]string{"FOO": "bar"},
	}
	args := Assemble("alpine", p, Options{}, "alpine", nil)
	containsSeq(t, args, "-e", "HOME=/home/user")
	containsSeq(t, args, "-e", "FOO=bar")
}

func TestAssembleExtraArgsAndPresetArgs(t *testing.T) {
	p := config.Preset{
		Image:      "alpine",
		Entrypoint: "sh",
		Command:    []string{"-c"},
	}
	args := Assemble("alpine", p, Options{}, "alpine", []string{"echo", "hi"})
	containsSeq(t, args, "--entrypoint", "sh")
	// After the image, preset.Command comes first then extraArgs.
	containsSeq(t, args, "alpine", "-c", "echo", "hi")
}

func TestAssembleOverrides(t *testing.T) {
	p := config.Preset{Image: "alpine", Entrypoint: "sh", User: "default", Home: "/old"}
	args := Assemble("alpine", p, Options{
		Entrypoint:  "bash",
		User:        "0:0",
		Home:        "/new",
		ExtraMounts: []string{"/host:/container", "/var/run/docker.sock:/var/run/docker.sock"},
		ExtraPorts:  []string{"8080:80"},
		ExtraEnv:    map[string]string{"K": "V"},
	}, "alpine", nil)

	containsSeq(t, args, "--entrypoint", "bash")
	containsSeq(t, args, "-u", "0:0")
	containsSeq(t, args, "-e", "HOME=/new")
	containsSeq(t, args, "-v", "/host:/container")
	containsSeq(t, args, "-p", "8080:80")
	containsSeq(t, args, "-e", "K=V")
	containsSeq(t, args, "-v", "/var/run/docker.sock:/var/run/docker.sock")
}

func TestAssembleMountsRO(t *testing.T) {
	p := config.Preset{Image: "alpine", Mounts: []string{"/host:/container:ro"}}
	args := Assemble("alpine", p, Options{}, "alpine", nil)
	containsSeq(t, args, "-v", "/host:/container:ro")
}

func TestUniqueContainerNameSanitizes(t *testing.T) {
	if got := uniqueContainerName("OpenCode/AI Assistant"); !strings.HasPrefix(got, "opencode-ai-assistant-") {
		t.Fatalf("containerName = %q", got)
	}
}

func containsSeqString(args []string, marker string, prefix string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == marker && strings.HasPrefix(args[i+1], prefix) {
			return true
		}
	}
	return false
}

func TestExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	got := expandTilde("~/foo")
	if got != "/tmp/fakehome/foo" {
		t.Errorf("expandTilde = %q", got)
	}
	// no-op on non-tilde
	if expandTilde("/abs") != "/abs" {
		t.Error("should pass through absolute paths")
	}
}

func TestExpandMount(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	// host side is tilde-expanded; container side with :ro preserved.
	got := expandMount("~/data:/data:ro")
	if got != "/tmp/fakehome/data:/data:ro" {
		t.Errorf("expandMount = %q", got)
	}
}

func TestPrintQuoting(t *testing.T) {
	args := []string{"run", "--entrypoint", "bash", "alpine", "-c", "echo hi"}
	quoted := quoteAll(args)
	// Every arg is unconditionally single-quoted so the emitted command is
	// unambiguous when pasted into a shell.
	for i, q := range quoted {
		if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
			t.Errorf("arg %d not quoted: %q", i, q)
		}
	}
	if quoted[len(quoted)-1] != "'echo hi'" {
		t.Errorf("expected quoted arg, got %q", quoted[len(quoted)-1])
	}
}

func TestPrintGolden(t *testing.T) {
	// Sanity-check that Assemble + shellQuote produce a deterministic,
	// shell-pasteable command for a representative preset. The container
	// --name suffix is non-deterministic, so replace it before comparing.
	p := config.Preset{
		Image:      "alpine:3.20",
		Entrypoint: "sh",
		Command:    []string{"-c"},
		Env:        map[string]string{"K": "V"},
	}
	args := Assemble("alpine", p, Options{}, "alpine:3.20", []string{"echo $HOME"})
	// Drop the actual container name (not stable across runs).
	for i, a := range args {
		if a == "--name" && i+1 < len(args) {
			args[i+1] = "alpine-REDACTED"
			break
		}
	}
	quoted := strings.Join(quoteAll(args), " ")
	// Must be entirely wrapped in single quotes per arg so the final
	// positional 'echo $HOME' is not expanded by the shell.
	if !strings.Contains(quoted, `'echo $HOME'`) {
		t.Errorf("expected 'echo $HOME' literal, got:\n%s", quoted)
	}
	if !strings.Contains(quoted, "'--name' 'alpine-REDACTED'") {
		t.Errorf("expected quoted --name, got:\n%s", quoted)
	}
	if !strings.Contains(quoted, "'K=V'") {
		t.Errorf("expected quoted env, got:\n%s", quoted)
	}
}

func TestMissingHostDirs(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	missingDir := filepath.Join(dir, "missing")
	missingFile := filepath.Join(dir, ".gitconfig")

	p := config.Preset{
		Image: "alpine",
		Mounts: []string{
			existing + ":/c/exists",
			missingDir + ":/c/missing",
			missingFile + ":/c/file:ro",
		},
	}
	got := MissingHostDirs(p, Options{ExtraMounts: []string{missingDir + ":/c/dup"}})
	// Missing paths are reported as-is and duplicates are deduped.
	if len(got) != 2 || got[0] != missingDir || got[1] != missingFile {
		t.Fatalf("MissingHostDirs = %v, want [%s %s]", got, missingDir, missingFile)
	}
}

func TestEnsureHostDirsCreates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	var out strings.Builder
	if err := ensureHostDirs([]string{target}, strings.NewReader("\n"), &out, true); err != nil {
		t.Fatalf("EnsureHostDirs: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("target is not a directory")
	}
}

func TestEnsureHostDirsSkipsWithoutTerminal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	var out strings.Builder
	if err := ensureHostDirs([]string{target}, strings.NewReader(""), &out, false); err != nil {
		t.Fatalf("EnsureHostDirs: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not be created, stat err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func TestEnsureHostDirsAbortOnDecline(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	var out strings.Builder
	err := ensureHostDirs([]string{target}, strings.NewReader("n\n"), &out, true)
	if err == nil {
		t.Fatal("expected abort error")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target should not be created, stat err = %v", statErr)
	}
}

func TestEnsureHostDirsNoop(t *testing.T) {
	// No missing paths: should do nothing and not prompt.
	var out strings.Builder
	if err := ensureHostDirs(nil, strings.NewReader(""), &out, true); err != nil {
		t.Fatalf("EnsureHostDirs(nil): %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func contains(args []string, needle string) bool {
	for _, a := range args {
		if a == needle {
			return true
		}
	}
	return false
}
