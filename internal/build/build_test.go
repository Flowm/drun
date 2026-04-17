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
