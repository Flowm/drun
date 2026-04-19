# drun - docker run

A preset-driven wrapper around `docker run` for ad-hoc usage of tools.

## Features

- YAML presets for common tools; ships with sensible defaults, overridable per user
- Always runs as the host user (`--user $(id -u):$(id -g)`) so files stay yours
- Current directory mounted at `/cwd` and set as the working dir by default
- Transparent layering: declare extra packages under `x-drun-layer` and drun builds a local image on top of the base
- Ad-hoc mode: run any image with `-i <ref>` without writing a preset
- Override any preset field from the CLI (image, mounts, env, ports, entrypoint, user, home)
- `--print` for dry-runs, `--rebuild` to force a layer rebuild, `--prune` to clean up built images
- Single static Go binary, depends only on the `docker` CLI

## Install

```
go install github.com/flowm/drun/cmd/drun@latest
```

Or clone and `make install` (defaults to `~/.local/bin`).

Requires `docker` on `$PATH`.

## Container image

Releases also publish `ghcr.io/flowm/drun`, which packages the `drun` binary together with the Docker CLI.

Use it directly like this:

```
docker run --rm ghcr.io/flowm/drun:latest --help
docker run --rm -it -v /var/run/docker.sock:/var/run/docker.sock -v "$PWD":"$PWD" -w "$PWD" -u $(id -u):$(id -g) --group-add $(stat -c '%g' /var/run/docker.sock) -e HOME="$HOME" --tmpfs "$HOME:mode=1777" ghcr.io/flowm/drun:latest opencode
```

`drun` shells out to `docker run`, so when `drun` itself is running inside a container it still needs a Docker daemon to talk to. Mounting `/var/run/docker.sock` lets the containerized `drun` process use the host Docker daemon.
When `drun` runs inside a container, it still expands `$PWD` and `~` before calling the host Docker daemon. Bind the current working directory at the same absolute path, keep `HOME` set to the host's absolute home path so `~` expands correctly, and mount that path as a writable `tmpfs` in the wrapper container so no home-directory files are exposed there. The Docker CLI inside the wrapper still needs to create `~/.docker`, so the `tmpfs` must be writable for the selected UID. If the mounted Docker socket is group-owned, add that socket GID with `--group-add $(stat -c '%g' /var/run/docker.sock)` so the wrapper process can talk to the host daemon. Any real config or state mounts can still be passed later to the tool container itself.

## Help

```
drun — docker run, preset-driven.

Usage:
  drun [opts] <preset> [args...]        Run a preset; args after preset go to entrypoint
  drun [opts] -i <ref> [cmd...]         Run an ad-hoc image
  drun [opts] -i <ref> <preset> [args]  Run a preset with its image overridden
  drun --list                           List known presets
  drun --print <preset> [args...]       Dry-run: show docker commands
  drun --rebuild <preset> [args...]     Force rebuild of layer image, then run
  drun --prune                          Remove all drun/* local images
  drun -h, --help                       Show this help

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
```

## How it works

Every run applies these defaults:

- `--rm` and `-it` (or `-i` when stdin isn't a TTY)
- `--cap-drop=ALL --security-opt=no-new-privileges`
- `--name <preset>-<suffix>`
- `-v $(pwd):/cwd -w /cwd`
- `--user $(id -u):$(id -g)` unless the preset sets `user: default` or a specific uid:gid

If the preset declares an `x-drun-layer:`, drun builds a local image
`drun/<preset>:<hash>` once (installing the tools as root during the build),
then always runs that image as the host user at runtime. The hash covers
the base image, package manager, package list, and `environment.HOME`; change
any of those and a new image is built on next use.

## Preset schema

```yaml
services:
  <name>:
    image: <ref>                    # required
    entrypoint: <cmd>
    command: [default, args]
    environment:
      HOME: /home/user              # optional; ensures writable HOME
      KEY: VAL
    volumes: [host:container, ...]  # ~ expanded
    ports: [host:container, ...]
    user: default                   # "default" = omit --user; otherwise uid:gid
    x-drun-layer:                   # optional; triggers image build
      apk: [pkg, ...]
      apt: [pkg, ...]
      dnf: [pkg, ...]
      npm: [pkg, ...]               # global npm packages; can be combined with OS packages
```

## Config locations

Loaded in order; user presets fully replace shipped presets with the same name:

1. Embedded defaults (shipped in the binary, see [`internal/config/presets.yaml`](internal/config/presets.yaml))
2. `$XDG_CONFIG_HOME/drun/presets.yaml` or `~/.config/drun/presets.yaml`

## Examples

Run a shipped preset:

```
drun shellcheck script.sh
drun go build ./...
drun gdal gdalinfo some.tif
drun ripgrep TODO .
```

Run a preset with a tool layer (builds `drun/opencode:<hash>` on first use):

```
drun opencode
```

Preview what would be executed without running it:

```
drun --print opencode
```

Ad-hoc image with an on-the-fly layer:

```
drun -i alpine -l apk:jq,curl jq --version
```

Override a preset's image (e.g. try a newer version without editing config):

```
drun -i golang:1.26-alpine go build ./...
drun -i ghcr.io/anomalyco/opencode:canary opencode
```

Flags after the preset name are passed straight to the entrypoint:

```
drun shellcheck --external-sources script.sh
```

Extra mounts, env, and a custom entrypoint over a preset:

```
drun -v ~/data:/data -e DEBUG=1 --entrypoint bash shellcheck
```

Force a rebuild of a layer image (e.g. after the upstream base moved):

```
drun --rebuild opencode
```

Clean up all locally built `drun/*` images:

```
drun --prune
```

Example `~/.config/drun/presets.yaml` adding a new preset and overriding one:

```yaml
services:
  jq:
    image: alpine
    entrypoint: jq
    x-drun-layer:
      apk: [jq]

  opencode:
    image: ghcr.io/anomalyco/opencode
    entrypoint: opencode
    environment:
      HOME: /home/user
    volumes:
      - ~/.config/opencode:/home/user/.config/opencode
      - ~/.local/share/opencode:/home/user/.local/share/opencode
      - ~/.cache/opencode:/home/user/.cache/opencode
    x-drun-layer:
      apk: [git, npm]
```

## Layout

```
cmd/drun/main.go               CLI
internal/config/               YAML load + merge (embeds presets.yaml)
internal/build/                Dockerfile gen, hash, docker build
internal/run/                  docker run arg assembly
```

## Releasing

Releases are built by GoReleaser via GitHub Actions when a `v*` tag is pushed:

```
git tag v0.1.0
git push origin v0.1.0
```

The workflow (`.github/workflows/release.yml`) produces linux/darwin amd64 +
arm64 tarballs, a checksums file, and an auto-generated changelog on the
GitHub release. It also publishes a multi-arch container image
(`ghcr.io/flowm/drun`) built with [ko](https://ko.build). Configuration
lives in `.goreleaser.yaml`.

## Motivation

`drun` exists to keep the convenience of `docker run` aliases without the sprawl of maintaining them in shell config.

Compared to **Whalebrew**, `drun` is less magical and more explicit: it does not depend on image labels or installation shims, and every invocation is still just a transparent `docker run` you can inspect with `--print`.
Compared to **ccliwrapper**, `drun` stays **Docker-first** and works with a single static binary instead of targeting a Podman-specific workflow.
Unlike **Distrobox** or **Toolbx**, `drun` is not trying to provide a long-lived, host-integrated development environment — it is optimized for **ad-hoc, per-command tool execution**.

The goal is intentionally narrow: make containerized CLI tools feel as lightweight as aliases, while adding shareable presets, reproducible per-tool layering, and easy per-run overrides.
