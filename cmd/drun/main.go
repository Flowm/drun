package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/flowm/drun/internal/build"
	"github.com/flowm/drun/internal/config"
	"github.com/flowm/drun/internal/run"
)

// Populated via -ldflags at release time by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `drun — docker run, preset-driven.

Usage:
  drun [opts] <preset> [args...]        Run a preset; args after preset go to entrypoint
  drun [opts] -i <ref> [cmd...]         Run an ad-hoc image
  drun [opts] -i <ref> <preset> [args]  Run a preset with its image overridden
  drun --list                           List known presets
  drun --print <preset> [args...]       Dry-run: show docker commands
  drun --rebuild <preset> [args...]     Force rebuild of layer image, then run
  drun --prune                          Remove all drun/* local images
  drun -h, --help                       Show this help
  drun --version                        Print version

The first positional argument terminates drun flag parsing; everything after
is passed to the container entrypoint verbatim. All drun flags must appear
before the preset name.

Flags:
  -i, --image <ref>              Override/specify image
  -l, --layer <pm>:<pkg,...>     Add a layer (repeatable)
  -v, --mount <host:container>   Extra bind mount (repeatable)
  -e, --env KEY=VAL              Extra env var (repeatable)
  -p, --port <host:container>    Extra port mapping (repeatable)
  -u, --user <uid:gid|default>   Override user
      --entrypoint <cmd>         Override entrypoint
      --home <path>              Override HOME inside container
      --docker-socket            Mount /var/run/docker.sock
`

type flags struct {
	listMode    bool
	printMode   bool
	rebuildMode bool
	pruneMode   bool
	helpMode    bool
	versionMode bool

	image        string
	layers       []string
	mounts       []string
	envs         []string
	ports        []string
	entrypoint   string
	user         string
	home         string
	dockerSocket bool

	// positional after flag parsing
	rest []string
}

func main() {
	f, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "drun:", err)
		fmt.Fprintln(os.Stderr, "Run 'drun --help' for usage.")
		os.Exit(2)
	}
	if f.helpMode {
		fmt.Print(usage)
		return
	}
	if f.versionMode {
		fmt.Printf("drun %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	presets, err := config.Load()
	if err != nil {
		die(err)
	}

	switch {
	case f.listMode:
		cmdList(presets)
	case f.pruneMode:
		cmdPrune()
	default:
		if err := cmdRun(presets, f); err != nil {
			die(err)
		}
	}
}

func cmdList(presets config.Presets) {
	names := make([]string, 0, len(presets))
	for n := range presets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := presets[n]
		extra := ""
		if build.NeedsBuild(p) {
			extra = " [layered]"
		}
		fmt.Printf("%-14s %s%s\n", n, p.Image, extra)
	}
}

func cmdPrune() {
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}",
		"--filter", "reference="+build.ImageNamePrefix+"/*").Output()
	if err != nil {
		die(fmt.Errorf("list drun images: %w", err))
	}
	tags := strings.Fields(string(out))
	if len(tags) == 0 {
		fmt.Println("no drun/* images to prune")
		return
	}
	args := append([]string{"rmi"}, tags...)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die(err)
	}
}

func cmdRun(presets config.Presets, f *flags) error {
	var (
		p     config.Preset
		name  string
		extra []string
	)

	// Determine whether the first positional names a known preset. If so,
	// use it as the base (even when --image is also set, in which case the
	// image is overridden). Otherwise fall back to ad-hoc mode, which
	// requires --image.
	if len(f.rest) > 0 {
		if preset, ok := presets[f.rest[0]]; ok {
			name = f.rest[0]
			p = preset
			extra = f.rest[1:]
		}
	}
	if name == "" {
		if f.image == "" {
			if len(f.rest) == 0 {
				return fmt.Errorf("no preset given")
			}
			return fmt.Errorf("unknown preset %q", f.rest[0])
		}
		name = "adhoc"
		extra = f.rest
	}
	if f.image != "" {
		p.Image = f.image
	}

	// Merge CLI flags into preset.
	opts, err := flagsToOptions(f)
	if err != nil {
		return err
	}
	applyOptionsToPreset(&p, opts)

	if err := p.Validate(name); err != nil {
		return err
	}

	image := p.Image
	if build.NeedsBuild(p) {
		if f.printMode {
			build.PrintBuild(name, p)
			image = build.Tag(name, p)
		} else {
			tag, err := build.EnsureImage(name, p, f.rebuildMode)
			if err != nil {
				return err
			}
			image = tag
		}
	}

	args := run.Assemble(name, p, run.Options{}, image, extra)
	if f.printMode {
		run.Print(args)
		return nil
	}
	if missing := run.MissingHostDirs(p, run.Options{}); len(missing) > 0 {
		if err := run.EnsureHostDirs(missing, os.Stdin, os.Stdout); err != nil {
			return err
		}
	}
	return run.Exec(args)
}

// applyOptionsToPreset folds CLI-derived fields into the preset so the run
// assembly only needs to consult one struct.
func applyOptionsToPreset(p *config.Preset, opts run.Options) {
	p.Mounts = append(p.Mounts, opts.ExtraMounts...)
	p.Ports = append(p.Ports, opts.ExtraPorts...)
	if p.Env == nil && len(opts.ExtraEnv) > 0 {
		p.Env = map[string]string{}
	}
	for k, v := range opts.ExtraEnv {
		p.Env[k] = v
	}
	if opts.Entrypoint != "" {
		p.Entrypoint = opts.Entrypoint
	}
	if opts.User != "" {
		p.User = opts.User
	}
	if opts.Home != "" {
		p.Home = opts.Home
	}
	if opts.DockerSocket {
		p.DockerSocket = true
	}
	if len(opts.ExtraLayers) > 0 {
		if p.Layer == nil {
			p.Layer = map[string][]string{}
		}
		for pm, pkgs := range opts.ExtraLayers {
			p.Layer[pm] = append(p.Layer[pm], pkgs...)
		}
	}
}

func flagsToOptions(f *flags) (run.Options, error) {
	opts := run.Options{
		ExtraMounts:  f.mounts,
		ExtraPorts:   f.ports,
		Entrypoint:   f.entrypoint,
		User:         f.user,
		Home:         f.home,
		DockerSocket: f.dockerSocket,
	}
	if len(f.envs) > 0 {
		opts.ExtraEnv = map[string]string{}
		for _, e := range f.envs {
			k, v, ok := strings.Cut(e, "=")
			if !ok {
				return opts, fmt.Errorf("invalid --env %q (expected KEY=VAL)", e)
			}
			opts.ExtraEnv[k] = v
		}
	}
	if len(f.layers) > 0 {
		opts.ExtraLayers = map[string][]string{}
		for _, l := range f.layers {
			pm, pkgs, ok := strings.Cut(l, ":")
			if !ok {
				return opts, fmt.Errorf("invalid --layer %q (expected pm:pkg,pkg)", l)
			}
			opts.ExtraLayers[pm] = append(opts.ExtraLayers[pm], strings.Split(pkgs, ",")...)
		}
	}
	return opts, nil
}

func parseArgs(argv []string) (*flags, error) {
	f := &flags{}
	i := 0
	for i < len(argv) {
		a := argv[i]
		switch {
		case a == "--help" || a == "-h":
			f.helpMode = true
			return f, nil
		case a == "--version":
			f.versionMode = true
			return f, nil
		case a == "--list":
			f.listMode = true
		case a == "--prune":
			f.pruneMode = true
		case a == "--print":
			f.printMode = true
		case a == "--rebuild":
			f.rebuildMode = true
		case a == "--docker-socket":
			f.dockerSocket = true
		case a == "--image" || a == "-i":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.image = v
		case a == "--layer" || a == "-l":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.layers = append(f.layers, v)
		case a == "--mount" || a == "-v":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.mounts = append(f.mounts, v)
		case a == "--env" || a == "-e":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.envs = append(f.envs, v)
		case a == "--port" || a == "-p":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.ports = append(f.ports, v)
		case a == "--entrypoint":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.entrypoint = v
		case a == "--user" || a == "-u":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.user = v
		case a == "--home":
			v, err := takeVal(argv, &i, a)
			if err != nil {
				return nil, err
			}
			f.home = v
		case a == "--":
			f.rest = append(f.rest, argv[i+1:]...)
			return f, nil
		case strings.HasPrefix(a, "-") && a != "-":
			return nil, fmt.Errorf("unknown flag %q", a)
		default:
			// First positional terminates drun flag parsing; everything
			// after it is passed through to the container entrypoint.
			f.rest = append(f.rest, argv[i:]...)
			return f, nil
		}
		i++
	}
	return f, nil
}

func takeVal(argv []string, i *int, name string) (string, error) {
	if *i+1 >= len(argv) {
		return "", fmt.Errorf("%s requires a value", name)
	}
	*i++
	return argv[*i], nil
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "drun:", err)
	os.Exit(1)
}
