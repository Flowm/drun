//go:build !windows

package main

import (
	"reflect"
	"testing"

	"github.com/flowm/drun/internal/config"
	"github.com/flowm/drun/internal/run"
)

func TestParseArgsHelp(t *testing.T) {
	for _, a := range []string{"-h", "--help"} {
		f, err := parseArgs([]string{a})
		if err != nil {
			t.Fatalf("parseArgs(%q): %v", a, err)
		}
		if !f.helpMode {
			t.Errorf("parseArgs(%q): helpMode not set", a)
		}
	}
}

func TestParseArgsVersion(t *testing.T) {
	f, err := parseArgs([]string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.versionMode {
		t.Error("versionMode not set")
	}
}

func TestParseArgsPositionalTerminatesFlags(t *testing.T) {
	// After the first positional, remaining tokens pass through verbatim -
	// including things that look like flags.
	f, err := parseArgs([]string{"shellcheck", "--external-sources", "script.sh"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shellcheck", "--external-sources", "script.sh"}
	if !reflect.DeepEqual(f.rest, want) {
		t.Errorf("rest = %v, want %v", f.rest, want)
	}
}

func TestParseArgsFlagsBeforePreset(t *testing.T) {
	f, err := parseArgs([]string{"-i", "golang:1.26-alpine", "-e", "FOO=bar", "go", "build", "./..."})
	if err != nil {
		t.Fatal(err)
	}
	if f.image != "golang:1.26-alpine" {
		t.Errorf("image = %q", f.image)
	}
	if !reflect.DeepEqual(f.envs, []string{"FOO=bar"}) {
		t.Errorf("envs = %v", f.envs)
	}
	if !reflect.DeepEqual(f.rest, []string{"go", "build", "./..."}) {
		t.Errorf("rest = %v", f.rest)
	}
}

func TestParseArgsShortLong(t *testing.T) {
	// Every long flag that has a short form should parse the same.
	pairs := []struct {
		short, long, val string
		check            func(*flags) string
	}{
		{"-i", "--image", "img", func(f *flags) string { return f.image }},
		{"-u", "--user", "1:1", func(f *flags) string { return f.user }},
	}
	for _, p := range pairs {
		fs, err := parseArgs([]string{p.short, p.val})
		if err != nil {
			t.Fatalf("%s: %v", p.short, err)
		}
		fl, err := parseArgs([]string{p.long, p.val})
		if err != nil {
			t.Fatalf("%s: %v", p.long, err)
		}
		if p.check(fs) != p.val || p.check(fl) != p.val {
			t.Errorf("%s/%s not equivalent", p.short, p.long)
		}
	}
}

func TestParseArgsRepeatableFlags(t *testing.T) {
	f, err := parseArgs([]string{
		"-l", "apk:jq",
		"-l", "apk:curl",
		"-v", "/a:/a",
		"-v", "/b:/b",
		"-e", "A=1",
		"-e", "B=2",
		"-p", "80:80",
		"-p", "81:81",
		"alpine",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.layers, []string{"apk:jq", "apk:curl"}) {
		t.Errorf("layers = %v", f.layers)
	}
	if !reflect.DeepEqual(f.mounts, []string{"/a:/a", "/b:/b"}) {
		t.Errorf("mounts = %v", f.mounts)
	}
	if !reflect.DeepEqual(f.envs, []string{"A=1", "B=2"}) {
		t.Errorf("envs = %v", f.envs)
	}
	if !reflect.DeepEqual(f.ports, []string{"80:80", "81:81"}) {
		t.Errorf("ports = %v", f.ports)
	}
}

func TestParseArgsDoubleDash(t *testing.T) {
	f, err := parseArgs([]string{"-i", "alpine", "--", "--looks-like-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.rest, []string{"--looks-like-flag"}) {
		t.Errorf("rest = %v", f.rest)
	}
}

func TestParseArgsUnknownFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--nope"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseArgsMissingValue(t *testing.T) {
	if _, err := parseArgs([]string{"-i"}); err == nil {
		t.Fatal("expected error for missing value")
	}
}

func TestParseArgsModeFlags(t *testing.T) {
	f, err := parseArgs([]string{"--list"})
	if err != nil || !f.listMode {
		t.Errorf("--list: %v %+v", err, f)
	}
	f, err = parseArgs([]string{"--prune"})
	if err != nil || !f.pruneMode {
		t.Errorf("--prune: %v %+v", err, f)
	}
	f, err = parseArgs([]string{"--build", "alpine"})
	if err != nil || !f.buildMode {
		t.Errorf("--build: %v %+v", err, f)
	}
}

func TestParseArgsYesFlag(t *testing.T) {
	for _, a := range []string{"-y", "--yes"} {
		f, err := parseArgs([]string{a, "--prune"})
		if err != nil {
			t.Fatalf("parseArgs(%q): %v", a, err)
		}
		if !f.yes || !f.pruneMode {
			t.Errorf("parseArgs(%q): flags = %+v", a, f)
		}
	}
}

func TestParseArgsLatestFlag(t *testing.T) {
	f, err := parseArgs([]string{"--latest", "golang", "version"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.latest {
		t.Errorf("latest not set: %+v", f)
	}
	if !reflect.DeepEqual(f.rest, []string{"golang", "version"}) {
		t.Errorf("rest = %v", f.rest)
	}
}

func TestFlagsToOptionsEnvValidation(t *testing.T) {
	_, err := flagsToOptions(&flags{envs: []string{"NOEQUALS"}})
	if err == nil {
		t.Error("expected error for malformed --env")
	}
}

func TestFlagsToOptionsLayerValidation(t *testing.T) {
	_, err := flagsToOptions(&flags{layers: []string{"noformat"}})
	if err == nil {
		t.Error("expected error for malformed --layer")
	}
}

func TestFlagsToOptionsLayerFiltersEmptyTokens(t *testing.T) {
	opts, err := flagsToOptions(&flags{layers: []string{"apk:jq,,curl, ,git"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"jq", "curl", "git"}
	if !reflect.DeepEqual(opts.ExtraLayers["apk"], want) {
		t.Errorf("ExtraLayers[apk] = %v, want %v", opts.ExtraLayers["apk"], want)
	}
}

func TestFlagsToOptionsLayerParse(t *testing.T) {
	opts, err := flagsToOptions(&flags{layers: []string{"apk:jq,curl", "apk:git"}})
	if err != nil {
		t.Fatal(err)
	}
	got := opts.ExtraLayers["apk"]
	want := []string{"jq", "curl", "git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtraLayers[apk] = %v, want %v", got, want)
	}
}

func TestApplyOptionsToPresetDoesNotMutateShared(t *testing.T) {
	// A preset map returned from config.Load holds slices that share a
	// backing array. applyOptionsToPreset must copy before appending so
	// that mutating one caller's preset doesn't leak into the map.
	orig := config.Preset{
		Mounts: []string{"/a:/a"},
		Ports:  []string{"80:80"},
		Env:    map[string]string{"K": "V"},
		Layer:  map[string][]string{"apk": {"jq"}},
	}
	p := orig
	opts := run.Options{
		ExtraMounts: []string{"/b:/b"},
		ExtraPorts:  []string{"81:81"},
		ExtraEnv:    map[string]string{"K2": "V2"},
		ExtraLayers: map[string][]string{"apk": {"curl"}},
	}
	applyOptionsToPreset(&p, opts)

	if !reflect.DeepEqual(orig.Mounts, []string{"/a:/a"}) {
		t.Errorf("shared Mounts mutated: %v", orig.Mounts)
	}
	if !reflect.DeepEqual(orig.Ports, []string{"80:80"}) {
		t.Errorf("shared Ports mutated: %v", orig.Ports)
	}
	if !reflect.DeepEqual(orig.Env, map[string]string{"K": "V"}) {
		t.Errorf("shared Env mutated: %v", orig.Env)
	}
	if !reflect.DeepEqual(orig.Layer["apk"], []string{"jq"}) {
		t.Errorf("shared Layer mutated: %v", orig.Layer["apk"])
	}
}

func TestApplyOptionsToPresetDedupes(t *testing.T) {
	p := config.Preset{
		Mounts: []string{"/a:/a"},
		Ports:  []string{"80:80"},
		Layer:  map[string][]string{"apk": {"jq"}},
	}
	applyOptionsToPreset(&p, run.Options{
		ExtraMounts: []string{"/a:/a", "/b:/b"},
		ExtraPorts:  []string{"80:80", "81:81"},
		ExtraLayers: map[string][]string{"apk": {"jq", "curl"}},
	})
	if !reflect.DeepEqual(p.Mounts, []string{"/a:/a", "/b:/b"}) {
		t.Errorf("Mounts = %v", p.Mounts)
	}
	if !reflect.DeepEqual(p.Ports, []string{"80:80", "81:81"}) {
		t.Errorf("Ports = %v", p.Ports)
	}
	if !reflect.DeepEqual(p.Layer["apk"], []string{"jq", "curl"}) {
		t.Errorf("Layer[apk] = %v", p.Layer["apk"])
	}
}
