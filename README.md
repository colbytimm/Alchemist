# Alchemist

A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API), inspired by
[harlequin](https://github.com/tconbeer/harlequin): browse databases and containers,
write SQL, and page through results — without leaving the terminal.

> **Status: early development.** The project is being rebuilt iteration by iteration;
> see the [implementation plan](docs/plan/00-overview.md) for the roadmap and current
> progress.

## Build

```sh
make build        # → bin/alchemist
./bin/alchemist   # minimal shell (iteration 1); q to quit
```

## Development

```sh
make all          # fmt-check, lint, test, build
make help         # list all targets
```

Design notes, architecture, and the iteration-by-iteration plan live in
[docs/plan](docs/plan/00-overview.md).
