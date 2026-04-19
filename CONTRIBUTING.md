# Contributing

Thanks for your interest in `drun`. This is a small project with a
deliberately narrow scope, so please open an issue to discuss larger
changes before sending a PR.

## Development

Requirements:

- Go (version pinned in [`go.mod`](go.mod); CI uses `go-version-file`)
- `docker` on `$PATH` for manual end-to-end testing
- `make` for the convenience targets

Common tasks:

```
make build      # build ./bin/drun
make test       # go test ./...
make install    # install to ~/.local/bin (override with PREFIX=)
go vet ./...
go test -race -cover ./...
```

CI runs `go test -race -cover`, `go vet`, `staticcheck`, and a
`GOOS=windows go build ./...` smoke build. Please make sure those
pass locally before opening a PR.

## Code layout

```
cmd/drun/            CLI entry point and flag parsing
internal/config/     YAML load, merge, validation, embedded defaults
internal/build/      Dockerfile generation, hashing, docker build
internal/run/        docker run argv assembly
```

The CLI, `internal/run`, and `internal/build` are unix-only
(`//go:build !windows`). Only `internal/config` and the top-level
package compile on Windows, which is what the CI smoke build verifies.

## Style

- Idiomatic Go: prefer `slices`/`maps`/`cmp` over hand-rolled helpers.
- Keep the public surface small; anything internal goes under
  `internal/`.
- Tests live next to the code they cover. Prefer table tests for
  parsing and validation.
- Error messages are lower-case and don't end with punctuation, per
  Go conventions.

## Commits and PRs

- Use [Conventional Commits](https://www.conventionalcommits.org/) for
  commit subjects (e.g. `fix(run): ...`, `feat(build): ...`,
  `test: ...`, `ci: ...`).
- Keep commits focused; a good rule of thumb is "one logical change
  per commit, all tests green on every commit".
- PRs should describe the motivation and call out any user-visible
  behaviour change.

## Adding or changing shipped presets

`internal/config/presets.yaml` is embedded into the binary. When
adding a preset:

- Pin the image to a specific tag (not `latest`).
- Keep `x-drun-layer` package lists minimal and use upstream package
  names only — they are validated against an allow-list regex.
- Add a line to the README examples if the preset is notable.

## Security

Please report vulnerabilities privately; see
[`SECURITY.md`](SECURITY.md).
