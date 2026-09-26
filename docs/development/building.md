# 15. Building and Testing

- [15.1. Make targets](#151-make-targets)
- [15.2. The typing benchmark gate](#152-the-typing-benchmark-gate)
- [15.3. CI and releases](#153-ci-and-releases)

## 15.1. Make targets

```sh
make all               # fmt-check, lint, spell, test, build
make security          # gosec, govulncheck, gitleaks
make release-snapshot  # every release archive into dist/, nothing published
make help              # list all targets
```

Every tool runs at a version pinned in the `Makefile`, and CI calls the same targets,
so a green `make all` locally means a green quality job.

Integration tests run against the emulator:

```sh
make emulator-up
make test-integration
```

## 15.2. The typing benchmark gate

Typing speed is gated. `make bench-gate` runs the typing benchmarks five times each and
holds the median to `testdata/bench-baseline.txt`:

- allocations and bytes per keystroke may grow at most 5%;
- `query.Diagnose` must check a 2,000-line query within 5 ms;
- a 2,000-line buffer may cost at most 10× a 200-line one.

`make bench-baseline` records a new baseline, which is committed on its own.

## 15.3. CI and releases

| Workflow | Runs on | Does |
|---|---|---|
| `pr.yml` | pull requests to `main` | quality, benchmark, security, and emulator integration gates; `goreleaser check` and `actionlint` |
| `main.yml` | pushes to `main` | the same gates, then snapshot archives uploaded as build artifacts |
| `release.yml` | tags matching `v*` | the same gates, then a GitHub Release with archives, checksums, and changelog |

`main` is expected to be a protected branch that requires the `pr.yml` checks
(`gates / quality`, `gates / benchmarks`, `gates / security`, `gates / integration`,
`release-check`) to pass before a merge.

Cutting a release is one step:

```sh
git tag v0.1.0 && git push --tags
```

Design notes, architecture, and the iteration-by-iteration plan live in
[docs/plan](../plan/00-overview.md).

---

[← 14. Writing an Adapter](adapters.md) · [Contents](../README.md)
