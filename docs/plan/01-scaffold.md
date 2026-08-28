# Iteration 1 — Scaffold + Runnable Shell

## Goal

A buildable, lintable, testable Go module that produces an `alchemist` binary which
opens a minimal bubbletea shell (themed ASCII logo + version) and exits cleanly.
Every later iteration builds on this quality gate.

## Scope

- `go.mod` — module `github.com/colbytimm/alchemist`, Go 1.24+.
  Dependencies: `spf13/cobra`, `charmbracelet/bubbletea`, `charmbracelet/lipgloss`,
  `stretchr/testify`.
- `main.go` — thin entry: construct root command, execute, exit non-zero on error.
- `app/app.go` — `Name`, `Version`, `BuildDate` variables injected via ldflags.
- `cmd/root.go` — cobra root command; running `alchemist` starts a minimal
  bubbletea program; `--version` prints version/build info.
- `internal/tui/app.go` — placeholder root model: renders logo + version + quit hint;
  `q` / `ctrl+c` quits.
- `internal/theme/theme.go` — the theme is the **color profile and icon set only**
  (components keep plain names). Alchemy-inspired palette as `lipgloss.AdaptiveColor`
  (light/dark aware), shared styles, icon glyphs (database, container, expanded/collapsed
  chevrons, success ✓ / failure ✗), and the ASCII logo:
  - **Gold** `#D4A017` / `#F5C542` — primary accent, focused borders, logo
  - **Copper** `#B87333` / `#D48F52` — secondary accent, selected rows
  - **Verdigris** `#43B3AE` / `#5FD3CE` — success, RU stats
  - **Amethyst** `#6C3FA0` / `#9B6FD0` — keywords, highlights
  - **Parchment** `#5C4B37` / `#E8DCC8` — body text
  - **Cinnabar** `#C0392B` / `#E74C3C` — errors
- `Makefile` — targets: `all` (fmt-check lint test build), `build` (ldflags version from
  `git describe`), `test`, `coverage-html`, `fmt`, `fmt-check`, `vet`, `lint`,
  `security` (gosec + govulncheck + gitleaks), `clean`, `install-tools`, `help`.
- `.golangci.yml` — errcheck, staticcheck, govet, ineffassign, unused, misspell, gofmt,
  goimports, revive, errname, errorlint, bodyclose, gosec, gocritic, whitespace,
  unconvert, unparam, godot, gocognit, nilnil — plus **depguard**: deny
  `internal/tui/**` → `internal/adapter/cosmos`.
- `.gitignore`, `README.md` stub linking to `docs/plan/`.

## Out of scope

- Any Cosmos/adapter code, panes, config, history. The shell renders static content only.

## Steps

1. `go mod init github.com/colbytimm/alchemist`; add dependencies.
2. Write `internal/theme` (palette, styles, logo) — no dependencies on other packages.
3. Write the placeholder `internal/tui/app.go` model (Init/Update/View, quit keys).
4. Write `app/app.go`, `cmd/root.go`, `main.go`; wire `--version`.
5. Port the Makefile and golangci config from the `main` branch, updated for
   golangci-lint v2 schema and the depguard rule.
6. Add tests (below), `.gitignore`, README stub.

## Testing

**Unit** (`cmd/test/`, `internal/theme/test/`, `internal/tui/test/`):
- Root command constructs; `--version` output contains `app.Version`.
- Theme styles render non-empty output; palette colors are adaptive pairs.
- Root model: `q` and `ctrl+c` produce `tea.Quit`; `View()` contains the logo.

**Manual checklist:**
- [ ] `make all` passes clean.
- [ ] `./bin/alchemist` shows the gold logo in a full-screen shell; `q` exits, terminal restored.
- [ ] `./bin/alchemist --version` prints name, version, build date.

## Acceptance criteria

- `make all` green on a fresh clone with only Go installed (lint tools fetched via
  `go run` / `install-tools`).
- Binary runs and quits cleanly on macOS (darwin/arm64) and Linux.
- No package-global mutable state outside `app` ldflags variables.
