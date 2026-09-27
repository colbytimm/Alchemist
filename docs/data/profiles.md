# Profiles and accounts

## Profiles

A profile is a named connection in `~/.config/alchemist/config.toml`. The file holds
endpoints, never keys. The connect form creates profiles, and so do the `profile`
commands:

```sh
alchemist emulator start
alchemist profile add prod --endpoint https://myaccount.documents.azure.com:443/ --default
alchemist profile list
alchemist profile set-key prod
alchemist profile set-read-only prod false
alchemist profile remove emulator
```

`alchemist emulator start` adds the `emulator` profile for the
[local emulator](emulator.md). `profile add` asks for the key and stores it in the OS
keychain (Keychain on macOS, Secret Service on Linux, Credential Manager on Windows).
`profile remove` deletes the profile and its key, and keeps its saved queries and
snapshots unless you pass `--purge`.

For an emulator you run yourself, `--well-known-key` uses the emulator's published key
and asks for none. It works only for an endpoint on this machine:

```sh
alchemist profile add local --endpoint http://localhost:8081 --well-known-key
```

You can also edit the file directly:

```toml
default_profile = "emulator"

[profiles.emulator]
adapter = "cosmos"
endpoint = "http://localhost:8081"
well_known_key = true
database = "sales"

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

See [configuration](../reference/configuration.md) for every setting.

## Switching accounts

A session starts on the profile named on the command line, or the default profile.
`ctrl+g` opens the account switcher from anywhere:

| Key | Action |
|---|---|
| `enter` | switch to the selected account, connecting if needed |
| `a` | add account: the connect form, empty |
| `x` | disconnect the selected account |
| `/` | filter by name or endpoint |
| `esc` | clear the filter, or close |

If an account cannot connect, the session stays where it was and the switcher shows
why. If its key is missing, the connect form opens. Accounts you switch away from stay
connected, so switching back is instant.

The editor is shared across accounts. A query always runs on the current account, and
naming another account in `FROM` is refused.

## Keys

Alchemist looks for a profile's key in this order:

1. the emulator's published key, for a profile with `well_known_key = true`
2. the OS keychain (service `alchemist`, account `<name>`)
3. `ALCHEMIST_<NAME>_KEY`, with the profile name upper-cased and dashes as underscores
   (`my-emulator` becomes `ALCHEMIST_MY_EMULATOR_KEY`)
4. `COSMOS_CONNECTION_STRING`, used only for the profile whose endpoint matches the
   account in the string
5. the connect form

On machines without a keychain, such as containers and CI runners, set the environment
variable. `alchemist profile list` shows where each key comes from, never the key
itself. `alchemist profile set-key` on a `well_known_key` profile turns the setting off,
so the key you enter is the one used.

## Read-only accounts

Profiles are read-only unless the endpoint is `localhost`, `127.0.0.1` or `::1`. A
read-only account:

- refuses [batches](../language/transactions.md) that write, and
  [updates](../language/update.md) and [deletes](../language/delete.md) by query
- does not offer `ctrl+b`, or the catalog keys `n`, `c`, `d` and `t`
- cannot be the target of a [clone](cloning.md), though it can be the source

The status bar and switcher show `read-only` next to its name. To allow writes:

```sh
alchemist profile set-read-only prod false
```

or pass `--read-only=false` to `profile add`. `alchemist --read-only` makes every
account read-only for one session. The mock adapter is writable.
