# Iteration 27 — `alchemist emulator`

<!-- cspell:words pgcosmos cosmosdev HEALTHCHECK emulatorrun snapshotrun nerdctl -->

## Goal

Run the local Cosmos DB emulator from the binary, with no clone of this repo:

```
alchemist emulator start     # pull, run, wait until it answers, add the emulator profile
alchemist emulator seed      # load the sample sales, telemetry and hr databases
alchemist emulator           # open the app on it
```

Today the emulator exists only for people with the repo: `make emulator-up` runs
`docker compose` on `test/integration/docker-compose.yml`, `make emulator-seed` runs
`go run ./test/seed`, and the README then asks the user to paste the well-known key
into `profile add`. A user who downloaded a release archive has none of that.

This iteration adds an `emulator` command group that drives Docker or Podman, judges
readiness by a real request, and writes a profile that needs no key prompt and no
keychain. The sample data moves out of `test/` into the binary. The Makefile targets
and CI then call the command, so there is one way to run the emulator, and CI tests it.

What was verified, and where (2026-09-27, Docker 29.3.1, image label
`COSMOS_EMULATOR_RELEASE_VERSION=EN20260907`):

| Fact | Source |
|---|---|
| The image sets `PORT=8081`, `PROTOCOL=http`, `DATA_PATH=/data`, `ENABLE_TELEMETRY=true`, `ENABLE_EXPLORER=true`, `EXPLORER_PORT=1234`, `HEALTH_PORT=8080`. It declares no `VOLUME` and no `HEALTHCHECK`, and runs as `cosmosdev` (uid 1000) | `docker image inspect` |
| `/data` exists in the image, owned by `cosmosdev`, so a new named volume mounted there is writable (Docker copies the ownership on first mount) | `docker run --entrypoint sh … ls -ld /data` |
| When `--gateway-endpoint` is unset, the gateway advertises the host and port the client connected on, so `-p 49401:8081` works with no other setting | the image's `docker_entrypoint.sh --help` |
| `-p 127.0.0.1:18081:8081` with a volume at `/data`: the Go SDK listed databases on `http://localhost:18081` | run by hand |
| Seeded data survived `docker rm -f` and a new `docker run` on the same named volume (3 databases listed) | run by hand |
| Warm start sequence on the Go SDK: connection reset, then `EOF`, then `503` with `{"code":"ServiceUnavailable","message":"pgcosmos extension is still starting; retry request shortly"}`, then success, about 6 s in all | run by hand, probe listing databases |
| The container logs a status line every 5 s: `PostgreSQL=FAIL, Gateway=FAIL, Explorer=OK` until `PostgreSQL=OK, Gateway=OK, Explorer=OK` | `docker logs` |
| `GET /` (account read) can itself answer the 503 above. The internal `:8080/ready` (not published) answered 200 within 10 s of a start; it was not timed against a query, and Docker-level health is reported green before requests are served. Neither is used as a readiness signal | run by hand; reported |
| `docker stop` returns in about 1 s: the entrypoint honors `SIGTERM` | run by hand |
| With the daemon down, `docker info` and `docker version` exit 1 and print `Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?` | run in this environment |
| The container `make emulator-up` creates is named `alchemist-cosmos-emulator` and carries `com.docker.compose.*` labels | `docker inspect` |
| Idle memory is about 390 MiB | `docker stats` |
| The stuck state, 503 `pgcosmos extension is still starting` for good after a Docker daemon restart until the container is recreated, was observed in this repo's environment. It did **not** reproduce after a clean daemon restart, nor after a container that exited 255; both answered within 10 s | reported; not reproduced here |

## Commands

```
alchemist emulator [options]
alchemist emulator start  [--port <n>] [--timeout <d>] [--recreate] [--pull] [--runtime <name>]
alchemist emulator stop   [--runtime <name>]
alchemist emulator status [--runtime <name>]
alchemist emulator seed   [--profile <name>] [--replace]
alchemist emulator logs   [-f] [--tail <n>] [--runtime <name>]
alchemist emulator remove [--data] [--image] [--runtime <name>]
```

| Command | Effect |
|---|---|
| `emulator` | open the app on the `emulator` profile |
| `start` | pull the image if missing, create or start the container, wait until it answers a query, add or update the `emulator` profile |
| `stop` | stop the container; its data stays |
| `status` | report the runtime, the container, whether the endpoint answers, and the profile |
| `seed` | create the sample `sales`, `telemetry` and `hr` databases |
| `logs` | print the container's log |
| `remove` | delete the container; `--data` also deletes the data volume, `--image` the image |

There is no `install`. Pulling is the first half of `start`, and a user who pulls
without running has gained nothing. `start --pull` fetches a newer preview image.

### Why a bare `alchemist emulator` opens the app

`alchemist <profile>` launches a profile, and the README tells users to call their
profile `emulator` and run `./bin/alchemist emulator`. A Cobra subcommand named
`emulator` shadows that profile, so without care this change would break the
documented command. The group's own `RunE` therefore does exactly what the old command
did: it launches the TUI on the profile called `emulator`, through the same
`sessionFlags.run` the root uses. The group binds `sessionFlags` on its own `Flags()`,
so `alchemist emulator --read-only` and `--ascii` still work. It never touches Docker,
so it serves a native Windows emulator profile too. With no such profile it fails with
`no emulator profile: run alchemist emulator start`.

The other subcommand names (`start`, `stop`, …) cannot collide with a profile: only
the first word after `alchemist` is read as a profile.

## Driving the container runtime

### The CLI, not the Engine API

Alchemist runs the `docker` or `podman` binary with `os/exec`. It does not speak the
Engine API.

- The CLI already resolves where the daemon is: Docker contexts, `DOCKER_HOST`, Docker
  Desktop's per-user socket, Colima, OrbStack, the Windows named pipe, a Podman
  machine. Reimplementing that is most of the work of an API client and the part most
  likely to be wrong.
- The Engine API client (`github.com/docker/docker/client`) is a large dependency tree
  for seven calls. `CLEAN_CODE.md` asks for no dependency that does not reduce
  complexity.
- Podman's CLI accepts every command used here with the same flags. Its Docker API
  compatibility is a separate socket users must enable.

Compose is not used either. The compose plugin is not in every Docker install, and
`podman-compose` differs. One `run` command replaces the compose file.

### Choosing the binary

`--runtime docker|podman`, then `ALCHEMIST_CONTAINER_RUNTIME`, then `docker` if it is
on `PATH`, then `podman`. Flag over environment over default, as everywhere else.
Any other value is refused before anything runs, which also bounds what `exec` can be
handed (the `#nosec G204` justification). A `docker` that is really Podman's shim
works unchanged.

### What runs

The container is created once, then started and stopped:

```
docker run --detach \
  --name alchemist-cosmos-emulator \
  --label dev.alchemist.emulator=1 \
  --publish 127.0.0.1:8081:8081 \
  --volume alchemist-cosmos-emulator-data:/data \
  --env ENABLE_TELEMETRY=false \
  --env ENABLE_EXPLORER=false \
  mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview
```

| Choice | Why |
|---|---|
| Name `alchemist-cosmos-emulator` | the name CI's failure step already passes to `docker logs`, and the name `make emulator-up` has always used |
| Label `dev.alchemist.emulator=1` | Alchemist only starts, stops or deletes a container it created. A container of that name without the label (the one old `make emulator-up` made) is refused with `a container named alchemist-cosmos-emulator exists that Alchemist did not create: remove it with docker rm -f alchemist-cosmos-emulator, then run start again` |
| Published on `127.0.0.1` only | the key is public. An emulator on `0.0.0.0` is writable by anyone on the network |
| Host port `--port`, 8081 by default; container port always 8081 | the gateway advertises the port the client used (verified), so remapping needs no `--gateway-endpoint` |
| Named volume at `/data` | data survives `stop`, `start --recreate`, `start --pull` and `remove`. Only `remove --data` deletes it |
| `ENABLE_TELEMETRY=false` | the image sends usage data to Microsoft by default. A tool should not opt its user into a third party's telemetry. The docs say so |
| `ENABLE_EXPLORER=false`, port 1234 not published | Alchemist is the explorer. The log's `Explorer=` field is ignored |
| HTTP, no certificate | the image's default. The profile needs no `insecure_skip_verify` |
| No `--restart` policy | a container restarted by the daemon is the case that can come back stuck. `start` is where readiness is checked, so the user runs it |
| One image tag, `vnext-preview`, as a constant | the only Linux image that runs on Apple Silicon (iteration 3). Choosing another tag is out of scope |

`start` in each state of the container:

| State | `start` does |
|---|---|
| absent | check the host port is free, pull if the image is missing, `run`, wait |
| stopped (`created`, `exited`) | `start`, wait |
| running | wait. A running container may be the stuck one, so `start` is also the readiness check |
| `--recreate` | `rm -f` the container (the volume stays), then as absent |
| `--pull` | `pull`; if the image ID differs from the container's, recreate |
| `--port` differs from the published port | refused: `the container publishes port 8081: pass --recreate to move it to 9081` |
| not ours | refused, as in the table above |

`stop` runs `docker stop`. `remove` runs `docker rm -f`, then `docker volume rm` with
`--data` and `docker image rm` with `--image`. `logs` runs `docker logs [--follow]
--tail <n>` (200 by default) and streams it to the command's output.

### When the runtime is missing or not running

`start`, `stop`, `logs` and `remove` first run `<runtime> info` and discard its
output. `status` runs it too, but prints the error on its runtime line and still
probes the profile's endpoint, so it reports on a native Windows emulator. `seed` and
the bare form never run it. `info` exits non-zero for both Docker and Podman when the daemon or the
Podman machine cannot be reached (verified for Docker).

| Situation | Error |
|---|---|
| neither binary on `PATH` | `emulator: found neither docker nor podman on PATH: install Docker Desktop (https://docs.docker.com/get-docker/) or Podman, then run alchemist emulator start` |
| `--runtime` names a binary not on `PATH` | `emulator: podman is not on PATH` |
| `info` fails, Docker | `emulator: docker is installed but not running: start Docker Desktop or the docker service, then try again: <first line of stderr>` |
| `info` fails, Podman | `emulator: podman cannot reach its machine: run podman machine start, then try again: <first line of stderr>` |
| host port taken | `emulator: port 8081 is in use on this machine: pass --port to use another` plus, on Windows with the native emulator installed, the line below |

The port check is a `net.Listen` on `127.0.0.1:<port>`, closed at once, run only
before a `run`. It is what turns Docker's opaque `bind: address already in use` into
an instruction.

### The native Windows emulator

Detected, not managed. On Windows, when
`%ProgramFiles%\Azure Cosmos DB Emulator\Microsoft.Azure.Cosmos.Emulator.exe` exists,
the port-in-use error and the no-runtime error add:

```
The Windows emulator is installed. To use it instead, start it, then run:
  alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify --well-known-key
```

Starting and stopping the Windows emulator means its own CLI, its own data directory
and admin rights. Two emulators on one port is the mistake worth catching. Once that
profile exists, the bare `alchemist emulator`, `seed` and `status` (its endpoint line)
serve the Windows emulator as they serve the container.

## Readiness

The container reports running, and its internal `/ready` answers 200, before a query
succeeds. Readiness is therefore one thing: **the emulator profile's connection
answers `Ping`**, which in the Cosmos adapter lists databases and so reaches
PostgreSQL. The probe is built in `cmd` from `Profiles.Open` and `Connection.Ping`, so
it uses the same key, TLS and settings path the app will.

`emulator.Wait` polls:

- every 2 s, each attempt under its own 10 s deadline (azcore retries a 503 inside an
  attempt; the deadline bounds that);
- until the `--timeout`, 5 minutes by default (the old `emulator-wait` allowed
  60 × 5 s). A first start after a pull takes longest;
- and on each poll reads the container state. A container that exits while being
  waited on fails at once: `emulator: the container exited (code 1): see alchemist
  emulator logs`.

The log is the second signal. Each poll reads `docker logs --tail 20` and keeps the
last line holding `PostgreSQL=`. Only the `PostgreSQL` and `Gateway` fields are
read. It feeds the progress line, written to stderr every 10 s:

```
waiting for the emulator (0:20): PostgreSQL=FAIL, Gateway=FAIL
```

It never decides readiness: the wording is the image's and may change.

On timeout the error names the last answer, the last status line, and what to do:

```
emulator: no answer from http://localhost:8081 after 5m0s
last answer: 503 Service Unavailable: pgcosmos extension is still starting
last status: PostgreSQL=OK, Gateway=OK
The emulator can stay like this after Docker restarts. Recreate the container, keeping your data:
  alchemist emulator start --recreate
If it is still not ready, start again with no data:
  alchemist emulator remove --data && alchemist emulator start
```

The two-step advice is deliberate. The stuck state was fixed by recreating a container
that kept its data inside itself. With the named volume, recreating keeps `/data`, and
whether the stuck state lives there is not known (see Open questions). Step 1 of this
plan tries to reproduce it and settles which advice comes first.

`start` never recreates on its own. The user decides when a container is thrown away.

`status` makes one probe attempt, with the 10 s deadline, and prints `ready`,
`starting: <last answer>` or `not answering: <error>`.

## The emulator profile

`start` writes, when it is missing:

```toml
[profiles.emulator]
adapter = "cosmos"
endpoint = "http://localhost:8081"
well_known_key = true
```

- **The key.** The emulator's key is fixed, public, and in Microsoft's documentation.
  The keychain is the wrong home for it: some machines have none (headless Linux, CI,
  containers), and storing a public constant there buys nothing. The environment
  variable works but is a step every user must take and every shell must keep. So a
  profile setting names the source instead of holding the key: `well_known_key = true`
  makes `SecretResolver` return `config.EmulatorKey`, first, with source
  `well-known`. The TOML still holds no key, and `profile list` shows `well-known` in
  its `KEY` column.
- `well_known_key` is refused on a non-local endpoint (`IsLocalEndpoint`), with
  `profile "x": well_known_key is only for an emulator on this machine`. The key on a
  remote endpoint is either a mistake or an emulator exposed to a network.
- **Writes.** `read_only` is left unset. An unset `read_only` on a local endpoint
  already allows writes (`Profile.IsReadOnly`, iteration 17). One rule, and
  `profile set-read-only emulator true` still works.
- **Default.** `Config.Put` makes the first profile the default, as today. On a new
  machine `alchemist` alone then opens the emulator.
- **An existing `emulator` profile.** One with `well_known_key` and a local endpoint
  is Alchemist's: its endpoint port follows `--port`. Any other is the user's and is
  left alone, with one line: `profile emulator exists and points at <endpoint>; left
  as is`. A profile made by the old README (`http://localhost:8081`, key in the
  keychain) keeps working unchanged.
- The profile name is the constant `emulator.ProfileName`. There is no `--profile` on
  `start`: the bare command opens that name, and a second emulator is out of scope.

`profile add --well-known-key` sets the same field and skips the key prompt. It is the
route for the native Windows emulator and any hand-run container. `profile set-key` on
a `well_known_key` profile clears the setting, since a stored key would otherwise never
be read, and says `profile emulator now uses the key you entered`.

`config.EmulatorKey` replaces the three copies of the key in `test/seed/main.go`,
`test/integration/tui_test.go` and `internal/adapter/cosmos/test/integration_test.go`,
keeping the `// #gitleaks:allow` annotation. It is the one exception the definition of
done already allows.

## Sample data in the binary

`test/seed` becomes `internal/sample`. It was a `main` package calling `azcosmos`
directly. It becomes a library that seeds through the adapter interfaces:

```go
package sample

// Target is the connection a seed writes through.
type Target struct {
    Catalog adapter.Catalog
    Admin   adapter.CatalogAdmin
    Writer  adapter.ItemWriter
}

type Seeded struct {
    Database, Container, PartitionKey string
    Items                             int
}

var ErrExists = errors.New("sample database exists")

func Names() []string // "sales", "telemetry", "hr"
func Seed(ctx context.Context, t Target, replace bool, report func(Seeded)) error
```

`replace` is a boolean parameter, which `CLEAN_CODE.md` discourages. Split it at
implementation into `Seed` and `Replace` if the body forks by more than one guard.

- Through `CatalogAdmin` and `ItemWriter`, not `azcosmos`: the seed then works against
  the mock adapter, so it is unit-tested without a network, and it uses the adapter's
  TLS and key handling instead of a second copy.
- `data.go` moves unchanged apart from export needs. The generated items are the ones
  the docs and every manual checklist use.
- **Refuses to overwrite by default.** The seeder today drops `sales`, `telemetry` and
  `hr` without asking. That was acceptable for a developer's test emulator; a user's
  emulator may hold a `sales` of their own. Without `--replace`, `seed` lists the
  databases that exist and stops: `sales and hr exist: pass --replace to drop and
  recreate them`. The Makefile passes `--replace`, keeping `make emulator-seed`'s
  behavior.
- `seed` connects the profile named by `--profile` (default `emulator`) and refuses a
  non-local endpoint before connecting, as the old seeder did. It is not a Docker
  command, so it seeds the native Windows emulator too.
- Output goes through `cmd.OutOrStdout()`, one line per container, as today:
  `sales.orders: 60 items, partitioned on /customerId`.

## Scope

- `internal/emulator/` (new):
  - `emulator.go`: the constants (`Image`, `ContainerName`, `VolumeName`,
    `ProfileName`, `DefaultPort`, the label), the sentinel errors (`ErrNoRuntime`,
    `ErrRuntimeDown`, `ErrNotManaged`, `ErrPortInUse`).
  - `runtime.go`: `Runtime{Name string; exec Exec}`, where `Exec` runs an argv and
    returns stdout, and a streaming variant for `pull` and `logs`. `DetectRuntime`
    (flag, env, `exec.LookPath`), `Check`, `Inspect` returning `Container{State,
    ImageID, HostPort, Managed}`, `Pull`, `Run`, `Start`, `Stop`, `Remove`,
    `RemoveVolume`, `RemoveImage`, `Logs`, `StatusLine`. The argv for each lives here
    and nowhere else.
  - `manager.go`: `Manager{Runtime, Probe, Progress io.Writer}` with `Start(ctx,
    StartOptions)`, which is the state table above, and `Status(ctx)`.
  - `wait.go`: `Wait`, `NotReadyError{Endpoint, Waited, LastAnswer, LastStatus}`,
    `ExitedError`.
  - `profile.go`: `Profile(port)` and `EnsureProfile(cfg, port) (config.Config,
    ProfileOutcome)`, pure.
  - `native.go`: `WindowsEmulatorPath() (string, bool)`, the path read from
    `ProgramFiles` and checked with `os.Stat`, false off Windows.
- `internal/sample/` (new): `data.go` from `test/seed/data.go`; `seed.go` as above.
- `internal/config`: `Profile.WellKnownKey` (`well_known_key`), its validation,
  `EmulatorKey`, `SourceWellKnown`, the resolver's first branch.
- `cmd/emulator.go` (new): `newEmulatorCmd(keyring)`, the bare `RunE`, and one
  `newEmulatorXCmd` per subcommand. `runtimeFlags` with a `bind` like `scopeFlags`.
  The probe built from `Profiles.Open` and `Ping`. `cmd/emulatorrun.go` if the file
  passes about 300 lines, as `snapshot.go` and `snapshotrun.go` split.
- `cmd/root.go`: `newEmulatorCmd(keyring)` joins `AddCommand`. `NewRootCmd`'s signature
  does not change: `Manager` is tested in its package, and `cmd` tests reach the
  runtime through `PATH`.
- `cmd/profile.go`: `--well-known-key` on `add`; `set-key` clears the setting.
- `test/seed/` deleted. `test/integration/docker-compose.yml` deleted.
- `test/integration/*_test.go`, `internal/adapter/cosmos/test/integration_test.go`:
  `wellKnownKey` becomes `config.EmulatorKey`.
- `Makefile`:
  - `emulator-up: build` runs `bin/$(BINARY_NAME) emulator start`.
  - `emulator-seed: build` runs `bin/$(BINARY_NAME) emulator seed --replace`.
  - `emulator-down: build` runs `bin/$(BINARY_NAME) emulator remove --data`.
  - `emulator-wait` and `EMULATOR_URL` are deleted: `start` waits.
- `.github/workflows/gates.yml`: `make emulator-up emulator-wait` becomes
  `make emulator-up`. The failure step's `docker logs alchemist-cosmos-emulator`
  stays, since the name is unchanged.
- `.claude/skills/go-style/references/repo.md`: the `test/integration/` line no longer
  says docker-compose.
- `cspell.config.yaml`: `pgcosmos`, `Podman`, and any other new word `make spell`
  reports.
- The documentation in the next section.
- `docs/plan/00-overview.md`: row 27 in the iteration table; `cmd/` and `internal/`
  lines in the architecture block gain `emulator` and `sample`.

## Documentation

The docs describe what ships, in the same change. Grep `emulator`, `seed`, `make ` and
`docker` across `README.md` and `docs/` (not `docs/plan/`) before and after; every hit
must be deliberate.

| File | Change |
|---|---|
| `README.md` | "Use the local emulator": needs Docker or Podman; `alchemist emulator start`, `alchemist emulator seed`, `alchemist emulator`; no key to paste; the profile allows writes because it is local; link to the new emulator page. The block uses `alchemist`, not `./bin/alchemist`, since this section is for binary users too. Delete the "needs a clone of the repository" sentence that iterations 24 to 26 add. "Quick start" keeps building from source |
| `docs/README.md` | line 6: the sample data comes from `alchemist emulator seed`. "Accounts and data" gains `[The local emulator](data/emulator.md)` |
| `docs/data/emulator.md` (new) | what `start` does and what it needs; the runtime choice and `ALCHEMIST_CONTAINER_RUNTIME`; port, `--port`, loopback only; where data lives and what deletes it (a table: `stop`, `--recreate`, `remove`, `remove --data`); telemetry off; seeding and `--replace`; the Windows emulator with its `profile add` line; troubleshooting: Docker not running, port in use, the stuck 503 and its two steps, the old compose container |
| `docs/data/profiles.md` | the `profile add emulator …` example becomes `alchemist emulator start`, with `--well-known-key` shown for a hand-run emulator; the example TOML's emulator profile matches what `start` writes; the key lookup list gains step 0, `well_known_key`; "Read-only accounts" unchanged |
| `docs/reference/cli.md` | a new `## alchemist emulator` section in the `snapshot` section's style: the synopsis block, a command table, an options table per subcommand that has options (`start`, `seed`, `logs`, `remove`, and `--runtime` once for all), an example block. The bare form's session options are listed as "the options of `alchemist`". `profile add`'s table gains `--well-known-key` |
| `docs/reference/configuration.md` | the profile settings table gains `well_known_key`, default `false`; the example TOML's emulator profile uses it; the environment table gains `ALCHEMIST_CONTAINER_RUNTIME`; "Files" gains a line that emulator data is in the Docker volume `alchemist-cosmos-emulator-data`, not on disk under Alchemist's directories |
| `docs/install.md` (from iteration 24, if it has landed) | `## Uninstall` gains the `alchemist emulator remove --data --image` line. `## Build from source` keeps the `make emulator-*` targets out: they are for contributors |
| `docs/reference/keys.md` | no change: no key binding is added. Confirm |
| `docs/using/*.md`, `docs/language/*.md`, `docs/data/catalog.md`, `cloning.md`, `snapshots.md` | no change expected: none names `make` or the seeder today. Confirm with the grep |
| `docs/images/` | no screenshot shows the emulator commands, and the profile keeps the name `emulator`, so the status bar in `workspace.png` and `first-query.png` stays true. Look at each image the edited pages embed; re-take any that no longer matches, against the emulator, as `eb25d6d` did |

Docs style, which the README and `docs/` were just rewritten in and which these edits
must match:

- Short plain sentences, one idea each, in the active voice.
- Tables for reference facts: flags, settings, states, errors.
- Sentence-case headings.
- No em dashes, no bold lead-ins, no marketing words ("seamless", "powerful",
  "simply").
- Read `docs/data/profiles.md` and `docs/reference/cli.md` before writing, and copy
  their shapes.

## Out of scope

- Managing the native Windows emulator (start, stop, reset). It is detected and named.
- Choosing an image or tag, the classic x64 image, HTTPS on the vNext image, and
  publishing the data explorer.
- A remote Docker host (`DOCKER_HOST=ssh://…`, a remote context): the port is published
  on that host's loopback. `start` works; the endpoint does not answer; the timeout
  says so. Not detected.
- More than one emulator, or a profile name other than `emulator` from `start`.
- A "use the well-known key" choice in the TUI connect form. The CLI covers it.
- Refusing profile names that equal a subcommand (`profile`, `snapshot`,
  `emulator`). Only `emulator` had a documented profile, and the bare form keeps it.
- Kubernetes, Testcontainers, compose files.

## Relationship to other iterations

- **3, Cosmos adapter.** `Ping` is the readiness probe. The vNext image choice and the
  HTTP endpoint are 3's.
- **6, profiles.** Adds a secret source ahead of the keychain, named by a profile
  setting. The TOML still holds no key; "no secret on disk" holds, because the key is
  public and lives in the binary as it already did in the tests.
- **9, CI.** The integration job's emulator now comes from the command. A regression
  in `start` fails CI, which the compose file never could.
- **14, multiple accounts.** The emulator profile is an ordinary account in the
  switcher.
- **17, 21, 22, writes.** Writes on the emulator rest on the existing local-endpoint
  rule; nothing new.
- **18, cloning.** `docs/data/cloning.md`'s "clone from prod into the local emulator"
  now has a one-command emulator to clone into.
- **19, snapshots.** `remove --data` deletes the container's data, never snapshots. A
  snapshot of the emulator taken before `remove --data` is the way to keep data across
  it, and `docs/data/emulator.md` says so.
- **24, 25, 26, packaging.** No code dependency; this iteration is what makes their
  binaries enough to run the emulator. Each of them adds a README sentence that the
  emulator section needs a clone of the repository. This iteration deletes that
  sentence, whichever of them landed. If 24's `docs/install.md` exists, its
  `## Uninstall` section gains one line: `alchemist emulator remove --data --image`
  before uninstalling frees the container, its data and the image.

## Steps

1. **Settle the stuck state.** Start the emulator with the named volume, `kill -9` the
   Docker daemon (not a clean stop), restart it, `docker start` the container, and
   probe for 10 minutes. Then recreate with the volume kept, and probe again. Record in
   this document whether it reproduced, and whether recreating with the volume
   recovers. That decides the order of the timeout message's two steps. If recreating
   with the volume does not recover, the message leads with `remove --data`, and
   `docs/data/emulator.md` says `--recreate` does not clear the stuck state.
2. `config`: `WellKnownKey`, validation, `EmulatorKey`, resolver branch, `profile add
   --well-known-key`, `set-key` clearing it; replace the three test copies of the key.
   Tests first.
3. `internal/sample` from `test/seed`, seeding through the adapter interfaces, with
   `--replace` semantics; unit tests against the mock. `cmd/emulator.go` with the bare
   form and `seed`. Delete `test/seed`; point `make emulator-seed` at the command.
   **Ships: `alchemist emulator seed` and the bare form against any emulator.**
4. `internal/emulator`: runtime detection, `Check`, the argv table, `Inspect`, the
   `start` state table in `Manager`, `Wait`, `EnsureProfile`, the Windows path. Unit
   tests with a fake `Exec`.
5. `cmd`: `start`, `stop`, `status`, `logs`, `remove`, `runtimeFlags`, the probe.
   **Ships the feature.**
6. Makefile targets, `gates.yml`, delete `docker-compose.yml`, `repo.md`. Run
   `make emulator-down emulator-up test-integration` locally and push to see CI green.
7. **Documentation**, in this change: every file in the Documentation table, in the
   docs style above. Grep `README.md` and `docs/` for `emulator`, `seed`, `make ` and
   `docker` and account for each hit. Check every image the edited pages embed and
   re-take any that is stale into `docs/images/`. Run `make spell` and add real words
   to `cspell.config.yaml`. Check every relative link in the edited files resolves to
   a file and, for `#anchors`, a heading.
8. `docs/plan/00-overview.md` row 27; walk the manual checklist; write "Implementation
   notes" here for whatever landed differently.

## Testing

**Unit, `internal/config`:**
- A profile with `well_known_key` resolves to `EmulatorKey` with source `well-known`,
  even with a keychain entry and `ALCHEMIST_EMULATOR_KEY` set.
- `well_known_key` on `https://myaccount.documents.azure.com:443/` is refused, on load
  and on `Put`.
- TOML round trip writes `well_known_key = true` and nothing that looks like a key.

**Unit, `internal/sample`** (mock adapter):
- `Seed` into an empty mock creates 3 databases and 9 containers with the item counts
  of `data.go` (60 orders, 300 events, …) and the partition keys it names; `report` is
  called once per container, in order.
- With `sales` present and `replace` false: `ErrExists`, the message names `sales`,
  and nothing is created or deleted.
- With `replace` true: `sales` is dropped and recreated; an unrelated database is
  untouched.
- An item whose partition key value is not a string is an error naming the item.

**Unit, `internal/emulator`** (fake `Exec` recording argv and returning scripted
output):
- `DetectRuntime`: flag beats env beats `docker` beats `podman`; neither on `PATH` is
  `ErrNoRuntime`; `--runtime nerdctl` is refused before any `exec`.
- `Check`: a failing `info` is `ErrRuntimeDown` carrying the first stderr line, with
  the Docker advice for `docker` and the machine advice for `podman`.
- `Run`'s argv is exactly the command above, for port 8081 and for `--port 9081`
  (`127.0.0.1:9081:8081`).
- `Inspect` parses absent, `created`, `running`, `exited`, and a container without the
  label (`Managed` false).
- `Manager.Start`, one test per row of the state table, asserting the argv sequence:
  absent pulls only when the image is missing; running issues no `start`; `--recreate`
  removes the container and never the volume; `--pull` recreates only when the image ID
  changed; a port mismatch and an unmanaged container are refused with zero mutating
  calls; a taken port (a listener opened by the test) is `ErrPortInUse` before any
  `run`.
- `Wait` with a probe failing twice with the 503 text, then succeeding, returns nil. A
  probe that never succeeds returns `NotReadyError` whose text holds the last answer,
  the last status line, `--recreate` and `remove --data`. A container that turns
  `exited` returns `ExitedError` on that poll. The status line parser reads
  `PostgreSQL=OK, Gateway=FAIL, Explorer=OK` and ignores `Explorer`.
- `EnsureProfile`: none, so added and default; ours on 8081 with `--port 9081`, so
  endpoint updated; the user's (no `well_known_key`), so untouched and reported.
- `WindowsEmulatorPath` is false when not on Windows, and on Windows follows
  `ProgramFiles` (set with `t.Setenv` to a temp dir holding the file).

**Unit, `cmd/test/emulator_test.go`** (the existing harness):
- Bare `emulator` with no profile: the `no emulator profile` error, and no TUI start.
- `emulator seed --profile local` against a profile with `adapter = "mock"`, a local
  endpoint and `well_known_key`: one line per container. Again without `--replace`:
  refused, naming the three databases.
- `emulator seed` on a non-local profile: refused before connecting.
- `emulator start` with `PATH` set to an empty temp dir and
  `ALCHEMIST_CONTAINER_RUNTIME` unset: the no-runtime error naming Docker and Podman.
- `profile add x --endpoint http://localhost:8081 --well-known-key` reads no stdin;
  `profile list` shows `well-known`; `set-key x` then clears the setting.

**Integration** (`//go:build integration`, `test/integration/emulator_test.go`, with
the emulator CI starts through `make emulator-up`):
- `emulator status` through `cmd.NewRootCmd` reports `ready`.
- `emulator start` on the running container returns within 15 s and changes nothing.
- `sample.Seed` with `replace` into the emulator, then `SELECT VALUE COUNT(1) FROM c`
  on `sales.orders` is 60 and on `telemetry.events` is 300.

Integration never stops or removes the emulator: the rest of the suite uses it.

**Manual checklist:**
- [ ] With the image removed: `alchemist emulator start` pulls, shows progress lines,
      says ready, and adds the `emulator` profile as the default on an empty config.
- [ ] `alchemist emulator`: the app opens on `emulator` with no key prompt; the status
      bar shows no `read-only`; `ctrl+b` is offered.
- [ ] `alchemist emulator --read-only`: the same session is read-only.
- [ ] `seed`: nine lines. `seed` again: refused, naming the three. `seed --replace`:
      nine lines.
- [ ] `stop`, `status` says stopped; `start`; the seeded data is there.
- [ ] `remove`, `start`: data is there. `remove --data`, `start`: no databases.
      `remove --image`: `docker images` no longer lists it.
- [ ] Docker Desktop quit (macOS) or `systemctl stop docker` (Linux): `start` prints
      the not-running error and exits 1.
- [ ] `PATH` without docker or podman: the install error.
- [ ] Rootless Podman on Linux, and Podman machine on macOS, with
      `ALCHEMIST_CONTAINER_RUNTIME=podman`: start, seed, open, stop, remove.
- [ ] `python3 -m http.server 8081` running: `start` refuses with the `--port` hint;
      `start --port 9081` fails on the existing container, `--recreate --port 9081`
      works, and the profile endpoint becomes `http://localhost:9081`.
- [ ] A container left by the old `make emulator-up`: `start` refuses with the
      `docker rm -f` line.
- [ ] Step 1's stuck state, if reproduced: `start` times out with the message;
      the first suggested step recovers.
- [ ] `docker kill alchemist-cosmos-emulator` while `start` waits: fails on the next
      poll with the exited error.
- [ ] Headless Linux with no Secret Service and no `ALCHEMIST_EMULATOR_KEY`: start,
      open, seed all work.
- [ ] Windows with Docker Desktop: start, open, seed.
- [ ] Windows with the native emulator installed and running: `start` names it; the
      suggested `profile add` line works; `seed` and the bare form use it.
- [ ] `alchemist profile list` shows `well-known` and no key; `make gitleaks` is clean.
- [ ] `make emulator-up test-integration emulator-down` passes; the CI integration job
      passes.
- [ ] A user following only the README's emulator section, with a release binary and
      Docker, reaches a query of `sales.orders`.

## Acceptance criteria

- A user with the release binary and Docker or Podman runs `alchemist emulator start`,
  `alchemist emulator seed` and `alchemist emulator`, and queries `sales.orders`, with
  no repo, no Makefile, no key typed, and no keychain.
- `start` reports ready only after a query through the profile succeeds. It never
  reports ready on container state, Docker health, or the log line alone.
- A timeout, a container that exits, a missing runtime, a stopped daemon, a taken
  port, and a container Alchemist did not create each end in one error that says what
  to do next.
- Alchemist never stops, removes or recreates a container without its label, and never
  deletes the data volume without `remove --data`.
- The emulator listens on the loopback interface only, with telemetry off.
- `alchemist emulator` with no subcommand opens the `emulator` profile, as it did
  before this iteration.
- `seed` never drops a database without `--replace`, and never runs against a
  non-local endpoint.
- The well-known key is in the source once, in `internal/config`, annotated for
  gitleaks. No config file, log or history line holds it.
- `test/seed` and `test/integration/docker-compose.yml` are gone; `make emulator-up`,
  `emulator-seed`, `emulator-down` and CI use the command.
- The docs describe the shipped behavior: every file in the Documentation table is
  updated or confirmed unchanged, every relative link and anchor in `README.md` and
  `docs/` resolves, no screenshot contradicts the text, and `make spell` passes.
- `make all` passes.

## Open questions

- Whether the stuck 503 lives in `/data`. If it does, recreating with the volume kept
  does not recover, and the advice leads with `remove --data`. Step 1 decides.
- Whether a newer `vnext-preview` reads a volume written by an older one. If not,
  `start --pull` can recreate into a container that never becomes ready. The timeout's
  second step covers it; the docs should say so if Step 1's test with two image
  versions shows the problem.
- Podman on Windows is expected to work unchanged but is not on the checklist for lack
  of a machine to try it on.
