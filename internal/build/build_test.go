package build

import (
	"strings"
	"testing"

	"github.com/flowm/drun/internal/config"
)

func TestNeedsBuild(t *testing.T) {
	tests := []struct {
		name string
		p    config.Preset
		want bool
	}{
		{"no-layer", config.Preset{Image: "alpine"}, false},
		{"empty-layer", config.Preset{Image: "alpine", Layer: map[string][]string{"apk": {}}}, false},
		{"has-layer", config.Preset{Image: "alpine", Layer: map[string][]string{"apk": {"jq"}}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsBuild(tc.p); got != tc.want {
				t.Fatalf("NeedsBuild = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTagDeterministic(t *testing.T) {
	p := config.Preset{
		Image: "alpine:3.20",
		Home:  "/home/user",
		Layer: map[string][]string{"apk": {"jq", "curl", "git"}},
	}
	t1 := Tag("foo", p)
	t2 := Tag("foo", p)
	if t1 != t2 {
		t.Fatalf("tag not stable: %s vs %s", t1, t2)
	}
	if !strings.HasPrefix(t1, "drun/foo:") {
		t.Fatalf("unexpected tag prefix: %s", t1)
	}
}

func TestTagPackageOrderInsensitive(t *testing.T) {
	p1 := config.Preset{Image: "alpine", Layer: map[string][]string{"apk": {"jq", "curl"}}}
	p2 := config.Preset{Image: "alpine", Layer: map[string][]string{"apk": {"curl", "jq"}}}
	if Tag("x", p1) != Tag("x", p2) {
		t.Fatal("tag should be order-insensitive for packages")
	}
}

func TestTagChangesWithInputs(t *testing.T) {
	base := config.Preset{Image: "alpine", Layer: map[string][]string{"apk": {"jq"}}}
	baseTag := Tag("x", base)

	diffs := []config.Preset{
		{Image: "debian", Layer: map[string][]string{"apk": {"jq"}}},                     // image change
		{Image: "alpine", Layer: map[string][]string{"apk": {"jq", "curl"}}},             // pkg added
		{Image: "alpine", Layer: map[string][]string{"apt": {"jq"}}},                     // pm change
		{Image: "alpine", Home: "/home/user", Layer: map[string][]string{"apk": {"jq"}}}, // home change
		{Image: "alpine", Home: "/home/user", Layer: map[string][]string{"apk": {"jq"}}, // mount change
			Mounts: []string{"~/.config/x:/home/user/.config/x"}},
	}
	for i, d := range diffs {
		if Tag("x", d) == baseTag {
			t.Errorf("case %d: tag unchanged despite input change", i)
		}
	}
}

func TestDockerfile(t *testing.T) {
	p := config.Preset{
		Image: "alpine:3.20",
		Home:  "/home/user",
		Layer: map[string][]string{"apk": {"jq", "curl"}},
	}
	df := Dockerfile(p)
	if !strings.Contains(df, "FROM alpine:3.20") {
		t.Error("missing FROM line")
	}
	if !strings.Contains(df, "USER 0:0") {
		t.Error("missing USER 0:0")
	}
	if !strings.Contains(df, "apk add --no-cache jq curl") {
		t.Errorf("missing apk install; got:\n%s", df)
	}
	if !strings.Contains(df, "mkdir -p /home/user") || !strings.Contains(df, "chmod 777 /home/user") {
		t.Errorf("missing home setup; got:\n%s", df)
	}
}

func TestDockerfilePrecreatesMountParents(t *testing.T) {
	p := config.Preset{
		Image: "alpine:3.20",
		Home:  "/home/user",
		Mounts: []string{
			"~/.config/opencode:/home/user/.config/opencode",
			"~/.local/share/opencode:/home/user/.local/share/opencode",
			"~/.local/state/opencode:/home/user/.local/state/opencode",
			"~/.cache/opencode:/home/user/.cache/opencode",
			"~/.gitconfig:/home/user/.gitconfig:ro",
		},
	}
	df := Dockerfile(p)
	// Every bind-mount parent under $HOME must be pre-created so Docker
	// doesn't auto-create them as root:root 0755.
	for _, want := range []string{
		"/home/user/.config",
		"/home/user/.local",
		"/home/user/.local/share",
		"/home/user/.local/state",
		"/home/user/.cache",
	} {
		if !strings.Contains(df, want) {
			t.Errorf("expected parent dir %q in Dockerfile; got:\n%s", want, df)
		}
	}
	// Must still chmod 777 them so any -u uid can write.
	if !strings.Contains(df, "chmod 777 ") {
		t.Errorf("missing chmod 777 line; got:\n%s", df)
	}
	// File-bind's directory (/home/user, == Home) must not be duplicated.
	// A naive implementation might emit it twice; verify there's exactly one
	// "mkdir -p" line and it mentions /home/user once.
	lines := strings.Split(df, "\n")
	mkdirLines := 0
	for _, l := range lines {
		if strings.Contains(l, "mkdir -p") {
			mkdirLines++
		}
	}
	if mkdirLines != 1 {
		t.Errorf("expected exactly 1 mkdir -p line, got %d; df:\n%s", mkdirLines, df)
	}
}

func TestDockerfileIgnoresMountsOutsideHome(t *testing.T) {
	p := config.Preset{
		Image:  "alpine",
		Home:   "/home/user",
		Mounts: []string{"~/.aws:/root/.aws"},
	}
	df := Dockerfile(p)
	if strings.Contains(df, "/root") {
		t.Errorf("mount outside $HOME leaked into mkdir list:\n%s", df)
	}
}

func TestDockerfileAptDnf(t *testing.T) {
	apt := Dockerfile(config.Preset{Image: "debian", Layer: map[string][]string{"apt": {"jq"}}})
	if !strings.Contains(apt, "apt-get update") || !strings.Contains(apt, "apt-get install -y --no-install-recommends jq") {
		t.Errorf("apt dockerfile wrong:\n%s", apt)
	}
	if !strings.Contains(apt, "rm -rf /var/lib/apt/lists/*") {
		t.Errorf("apt missing cleanup:\n%s", apt)
	}

	dnf := Dockerfile(config.Preset{Image: "fedora", Layer: map[string][]string{"dnf": {"jq"}}})
	if !strings.Contains(dnf, "dnf install -y jq") || !strings.Contains(dnf, "dnf clean all") {
		t.Errorf("dnf dockerfile wrong:\n%s", dnf)
	}
}

func TestDockerfileNoLayer(t *testing.T) {
	df := Dockerfile(config.Preset{Image: "alpine"})
	if strings.Contains(df, "RUN ") {
		t.Errorf("unexpected RUN in layerless dockerfile:\n%s", df)
	}
}
