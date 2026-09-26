# 1. Getting Started

- [1.1. Building from source](#11-building-from-source)
- [1.2. A first look, with fixture data](#12-a-first-look-with-fixture-data)
- [1.3. Connecting to an account](#13-connecting-to-an-account)
- [1.4. The local emulator](#14-the-local-emulator)
- [1.5. Sample data](#15-sample-data)

## 1.1. Building from source

There are no release builds yet, so build from source with Go 1.26 or newer:

```sh
git clone https://github.com/colbytimm/Alchemist.git
cd Alchemist
make build
```

The binary lands in `bin/alchemist`.

## 1.2. A first look, with fixture data

The mock adapter serves in-memory fixture data and needs no account, key, or
network:

```sh
./bin/alchemist --adapter mock
```

It is writable, so every feature in this manual can be tried against it. `q` quits.

## 1.3. Connecting to an account

To run against a real account, start without the flag:

```sh
./bin/alchemist
```

The first run opens the connect form: name the profile, enter the account endpoint
and key, choose whether to remember the key in the OS keychain, and press `enter`.
The profile is saved to `config.toml`; the key goes to the keychain or nowhere. Every
run after that connects straight into the catalog, and `alchemist prod` picks a
profile by name.

A profile for a real account is [read-only](../data/profiles.md#104-read-only-accounts)
until you say otherwise. [Chapter 10](../data/profiles.md) covers profiles, keys, and
switching accounts in full.

## 1.4. The local emulator

`make emulator-up` starts the Cosmos DB emulator in Docker, serving HTTP on port 8081.
Add a profile for it and paste the emulator's
[well-known key](https://learn.microsoft.com/azure/cosmos-db/emulator#authentication)
when prompted:

```sh
make emulator-up
./bin/alchemist profile add emulator --endpoint http://localhost:8081
./bin/alchemist emulator
```

The emulator's endpoint is local, so its profile allows writes without being told.
`make emulator-down` stops it and discards its data.

## 1.5. Sample data

`make emulator-seed` loads the emulator with `sales`, `telemetry`, and `hr` databases.
It replaces databases of those names and only runs against localhost. The examples in
this manual run against them.

The data is uneven on purpose, so that outer joins have rows to pad: orders name
customers that do not exist and one customer has no order, most devices have no
alert, orders carry `lines` and `tags`, and employees name their `managerId`.

---

[Contents](../README.md) · [2. A Tour of the Workspace →](workspace.md)
