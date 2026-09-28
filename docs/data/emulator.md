# The local emulator

`alchemist emulator` runs the Azure Cosmos DB emulator in a container on your machine.
It needs Docker or Podman, and nothing else: no clone of this repository, and no key.

```sh
alchemist emulator start
alchemist emulator seed
alchemist emulator
```

## Starting it

`alchemist emulator start` does four things:

1. It pulls the emulator image if it is missing. The first pull is about 600 MB.
2. It creates the container `alchemist-cosmos-emulator`, or starts it if it exists.
3. It adds the `emulator` profile to `config.toml`, or moves it to a new port.
4. It waits until a query through that profile succeeds, and then says the emulator
   is ready.

A container that is already running still gets step 4, so `start` is also the way to
check it. `start` waits 5 minutes by default. `--timeout` changes that.

The profile it adds looks like this:

```toml
[profiles.emulator]
adapter = "cosmos"
endpoint = "http://localhost:8081"
well_known_key = true
```

`well_known_key` connects with the emulator's
[published key](https://learn.microsoft.com/azure/cosmos-db/emulator#authentication).
The key is not a secret, so the profile needs no keychain and asks for nothing. The
endpoint is local, so the profile allows writes. If it is your first profile, it
becomes the default.

If a profile called `emulator` already exists without `well_known_key`, `start` leaves
it as it is and uses it.

`alchemist emulator` with no subcommand opens the app on the `emulator` profile. It
takes the options of `alchemist`, such as `--read-only`.

## Docker or Podman

Alchemist runs the `docker` or `podman` command. It picks the first of:

| Source | Example |
|---|---|
| `--runtime` | `alchemist emulator start --runtime podman` |
| `ALCHEMIST_CONTAINER_RUNTIME` | `export ALCHEMIST_CONTAINER_RUNTIME=podman` |
| `docker`, if it is on `PATH` | |
| `podman`, if it is on `PATH` | |

Any other runtime is refused.

## Port and network

The emulator listens on port 8081 by default. `--port` picks another:

```sh
alchemist emulator start --port 9081 --recreate
```

A container keeps the port it was created with. To move it, pass `--recreate` with
`--port`. The profile's endpoint follows.

The port is published on `127.0.0.1` only. The emulator's key is public, so an
emulator open to the network would be open to anyone on it.

The container runs with telemetry off. The image sends usage data to Microsoft unless
told not to.

## Data

The emulator's data lives in the volume `alchemist-cosmos-emulator-data`, not in the
container. It survives everything but `remove --data`:

| Command | Container | Data |
|---|---|---|
| `alchemist emulator stop` | stopped | kept |
| `alchemist emulator start --recreate` | deleted and created again | kept |
| `alchemist emulator start --pull` | created again if the image changed | kept |
| `alchemist emulator remove` | deleted | kept |
| `alchemist emulator remove --data` | deleted | deleted |

`remove --image` also deletes the image. To keep the data across `remove --data`,
[snapshot](snapshots.md) its containers first. Snapshots live on disk, and `remove`
never touches them.

## Sample data

`alchemist emulator seed` creates three databases:

| Database | Containers |
|---|---|
| `sales` | `customers`, `orders`, `archive`, `products` |
| `telemetry` | `devices`, `events`, `alerts` |
| `hr` | `departments`, `employees` |

The examples in these docs use them. `seed` refuses to run if any of the three exists.
`--replace` drops and creates them again, and leaves every other database alone.

`seed` connects the `emulator` profile, or the one named by `--profile`. It refuses a
profile whose endpoint is not on this machine.

## The Windows emulator

Alchemist does not start or stop the Windows emulator. When it is installed, `start`
mentions it if the port is taken or no runtime is found. To use it, start it, then add
a profile for it:

```sh
alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify --well-known-key
```

`alchemist emulator`, `seed` and `status` then work with it.

## Troubleshooting

`alchemist emulator status` shows the runtime, the container, whether the endpoint
answers, and the profile.

| Problem | What to do |
|---|---|
| `docker is installed but not running` | Start Docker Desktop, or the docker service, then run `start` again. |
| `podman cannot reach its machine` | Run `podman machine start`, then run `start` again. |
| `port 8081 is in use on this machine` | Stop what uses the port, or pass `--port`. |
| `a container named alchemist-cosmos-emulator exists that Alchemist did not create` | An older setup made it. Remove it with `docker rm -f alchemist-cosmos-emulator`, then run `start` again. |
| `the container exited` | Run `alchemist emulator logs` to see why. |
| `no answer from http://localhost:8081` | See below. |

The emulator can stay unready after Docker restarts, answering
`pgcosmos extension is still starting` for good. Recreate the container. This keeps
your data:

```sh
alchemist emulator start --recreate
```

If it is still not ready, start again with no data:

```sh
alchemist emulator remove --data && alchemist emulator start
```

`alchemist emulator logs` prints the container's log. `-f` keeps following it.
