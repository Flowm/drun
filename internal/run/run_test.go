package run

import (
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
	args := Assemble(p, Options{}, "alpine", nil)

	if args[0] != "run" || args[1] != "--rm" {
		t.Errorf("expected run --rm prefix, got %v", args[:2])
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
	args := Assemble(p, Options{}, "alpine", nil)
	if contains(args, "-u") {
		t.Errorf("user:default should omit -u, got %v", args)
	}
}

func TestAssembleUserExplicit(t *testing.T) {
	p := config.Preset{Image: "alpine", User: "1234:5678"}
	args := Assemble(p, Options{}, "alpine", nil)
	containsSeq(t, args, "-u", "1234:5678")
}

func TestAssembleHomeAndEnv(t *testing.T) {
	p := config.Preset{
		Image: "alpine",
		Home:  "/home/user",
		Env:   map[string]string{"FOO": "bar"},
	}
	args := Assemble(p, Options{}, "alpine", nil)
	containsSeq(t, args, "-e", "HOME=/home/user")
	containsSeq(t, args, "-e", "FOO=bar")
}

func TestAssembleExtraArgsAndPresetArgs(t *testing.T) {
	p := config.Preset{
		Image:      "alpine",
		Entrypoint: "sh",
		Args:       []string{"-c"},
	}
	args := Assemble(p, Options{}, "alpine", []string{"echo", "hi"})
	containsSeq(t, args, "--entrypoint", "sh")
	// After the image, preset.Args come first then extraArgs.
	containsSeq(t, args, "alpine", "-c", "echo", "hi")
}

func TestAssembleOverrides(t *testing.T) {
	p := config.Preset{Image: "alpine", Entrypoint: "sh", User: "default", Home: "/old"}
	args := Assemble(p, Options{
		Entrypoint:   "bash",
		User:         "0:0",
		Home:         "/new",
		ExtraMounts:  []string{"/host:/container"},
		ExtraPorts:   []string{"8080:80"},
		ExtraEnv:     map[string]string{"K": "V"},
		DockerSocket: true,
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
	args := Assemble(p, Options{}, "alpine", nil)
	containsSeq(t, args, "-v", "/host:/container:ro")
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
	// Print goes to stdout; we just exercise quoteAll directly.
	quoted := quoteAll(args)
	if quoted[len(quoted)-1] != "'echo hi'" {
		t.Errorf("expected quoted arg, got %q", quoted[len(quoted)-1])
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
