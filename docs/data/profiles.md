# 10. Profiles and Accounts

- [10.1. Profiles](#101-profiles)
- [10.2. Switching accounts](#102-switching-accounts)
- [10.3. Where keys come from](#103-where-keys-come-from)
- [10.4. Read-only accounts](#104-read-only-accounts)

## 10.1. Profiles

Profiles are named connections kept in `config.toml` under `$XDG_CONFIG_HOME/alchemist`
(`~/.config/alchemist` by default). The file holds endpoints, never keys. The connect
form writes it for you; the `profile` commands do the same from a shell, for scripts
and CI:

```sh
alchemist profile add emulator --endpoint https://localhost:8081 --insecure-skip-verify
alchemist profile add prod --endpoint https://myaccount.documents.azure.com:443/ --default
alchemist profile list
alchemist profile set-key prod
alchemist profile set-read-only prod false
alchemist profile remove emulator
```

`profile add` prompts for the account key and stores it in the OS keychain (Keychain
on macOS, Secret Service on Linux, Credential Manager on Windows). `profile set-key`
replaces a key; `profile remove` deletes the profile and its keychain entry together,
keeping its saved queries and snapshots unless given `--purge`.

The file can also be written by hand:

```toml
default_profile = "emulator"

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # opened in the catalog on start

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

[Configuration](../reference/configuration.md) lists every setting, and
[Command-Line Programs](../reference/cli.md#alchemist-profile) every `profile` command.

## 10.2. Switching accounts

A session is on one account at a time, the one named on the command line or the
default profile: its databases fill the catalog and its name leads the status bar.
`ctrl+g` opens the account switcher, which lists every profile, from anywhere, the
editor included:

| Key | Action |
|---|---|
| `enter` | switch to the selected account, connecting it first if it is not connected |
| `a` | add account: the connect form, empty |
| `x` | disconnect the selected account |
| `/` | filter by name or endpoint |
| `esc` | clear the filter, or close |

An account connects the first time it is switched to, and a switch that cannot connect
leaves the session where it was, with the reason under the account's row. One whose
key is nowhere to be found opens the connect form instead. An account you leave stays
connected, with its tree and selected container kept, so switching back is instant.
`x` closes its connection and forgets its tree; the profile and its key stay where
they are.

The editor is shared by every account, so the same query can be run on two in turn.
The results pane's title names the account its rows came from, which is not always
the one the session is on now. A query always runs on the account the session is on:
`FROM staging.sales.orders` naming another account is refused with a message saying so.

## 10.3. Where keys come from

The key for profile `<name>` is looked up in this order:

1. The OS keychain (service `alchemist`, account `<name>`).
2. `ALCHEMIST_<NAME>_KEY`: the profile name upper-cased, with dashes as underscores
   (`my-emulator` → `ALCHEMIST_MY_EMULATOR_KEY`).
3. `COSMOS_CONNECTION_STRING`, a whole connection string, for ad-hoc use. It serves
   only a profile whose endpoint is the account the string names, so with several
   accounts in one session a string for one can never connect another.
4. The connect form, which asks for it and offers to store it in the keychain.

A machine with no keychain (a container, a CI runner, a server without a Secret
Service) falls through to the environment. `alchemist profile list` shows which source
each profile resolves to, and never the key itself, so its output is safe to share.

## 10.4. Read-only accounts

Writing to anything but a local emulator is a decision made per profile. A profile
with no `read_only` setting is read-only unless its endpoint is `localhost`,
`127.0.0.1` or `::1`. A read-only account:

- refuses every [batch](../language/transactions.md) that writes, and every
  [update](../language/update.md) or [delete](../language/delete.md) by query, before
  it reads anything;
- drafts nothing with `ctrl+b`;
- offers none of the [catalog's](catalog.md) `n`, `c`, `d` and `t`;
- is never a [clone's](cloning.md) target, though it is always a valid source.

The status bar and the account switcher say `read-only` beside its name. To allow
writes:

```sh
alchemist profile set-read-only prod false
alchemist profile add staging --endpoint https://staging.documents.azure.com:443/ --read-only=false
```

`alchemist --read-only` makes every account of one session read-only, whatever its
profile says; the flag only ever tightens. `--adapter mock` is writable.

---

[← 9. Deleting by Query](../language/delete.md) · [Contents](../README.md) · [11. Managing the Catalog →](catalog.md)
