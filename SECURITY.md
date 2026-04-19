# Security policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately via GitHub Security
Advisories on this repository ("Report a vulnerability" on the Security
tab) rather than opening a public issue. We will acknowledge receipt
within a few days and aim to ship a fix or mitigation for confirmed
issues as soon as reasonably possible.

Please include:

- A description of the issue and its impact
- Reproduction steps or a minimal proof of concept
- The `drun` version (`drun --help` header or commit SHA) and OS

## Threat model

`drun` is a thin wrapper around the `docker` CLI. It is designed to be
run locally by an interactive user against their own Docker daemon. It
is explicitly **not** a sandbox or a multi-tenant isolation boundary —
Docker itself provides the isolation, and any user able to talk to the
Docker daemon can already take over the host.

### Presets are code

`drun` loads presets from:

1. Embedded defaults shipped in the binary
   (`internal/config/presets.yaml`)
2. `$XDG_CONFIG_HOME/drun/presets.yaml` (or `~/.config/drun/presets.yaml`)

A preset can specify the image, entrypoint, command, environment,
mounts, ports, user, and an `x-drun-layer` package list that `drun`
installs at image-build time. **Treat a preset file as executable
code**: loading a malicious preset is equivalent to running arbitrary
commands inside a container that has your current directory bind
mounted at `/cwd`. Only use preset files from sources you trust, and
review diffs to your user preset file the same way you would review a
shell script.

### What drun validates

- Package names in `x-drun-layer` are validated against a conservative
  allow-list regex before being passed to the package manager, to
  prevent shell-metacharacter injection into the generated Dockerfile.
- The generated `docker run` argv is assembled as a `[]string` and
  passed to `exec` without a shell. `drun --build` prints the command
  with POSIX single-quote quoting so the printed line can be copy-pasted
  safely, but the actual execution path never goes through a shell.
- `drun --prune` requires interactive confirmation unless `-y` /
  `--yes` is passed, to avoid surprising removals when the command is
  mistyped or aliased.

### What drun does not do

- It does not sandbox the build. `docker build` runs with whatever
  privileges your Docker daemon grants. A malicious base image or
  package list can do anything a normal container build can do.
- It does not verify image signatures. Pin tags or digests in your
  presets if you need provenance guarantees (`image: foo@sha256:...`).
- It does not limit what a container can read via the `/cwd` bind
  mount or any additional `-v` mounts you declare. Treat container
  filesystem access as equivalent to host filesystem access for those
  paths.

## Supported versions

Only the latest tagged release is supported. Fixes land on `main` and
are cut into a new release.
