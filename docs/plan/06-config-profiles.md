# Iteration 6 — Config, Profiles, Secrets

## Goal

Replace the temporary connection flags with named profiles in a TOML config file, with
account keys kept out of plaintext: OS keychain first, environment variable fallback.

## Config design

`~/.config/alchemist/config.toml` (respect `$XDG_CONFIG_HOME`; `os.UserConfigDir()`):

```toml
default_profile = "emulator"

[profiles.emulator]
adapter = "cosmos"
endpoint = "https://localhost:8081"
insecure_skip_verify = true      # emulator self-signed cert only
database = "sales"               # optional default scope
page_size = 100

[profiles.prod]
adapter = "cosmos"
endpoint = "https://myaccount.documents.azure.com:443/"
```

**No secrets in TOML.** Key resolution order for profile `<name>`:
1. OS keychain via `zalando/go-keyring` — service `alchemist`, account `<name>`.
2. Env var `ALCHEMIST_<NAME>_KEY` (name upper-cased, dashes → underscores);
   generic `COSMOS_CONNECTION_STRING` honored as a last resort for ad-hoc use.
3. Interactive prompt on TUI start (masked input), with "store in keychain? (y/n)".

## Scope

- `internal/config/config.go` — load/parse/validate TOML (`BurntSushi/toml`), profile
  lookup, defaulting, clear errors with file/line context; `Write` support only for
  `profile add` scaffolding (comments preserved is a non-goal).
- `internal/config/secrets.go` — `SecretResolver` interface (keychain implementation +
  env implementation + chain); keyring behind an interface so tests use a fake and
  headless Linux (no dbus/secret service) degrades gracefully to env with a helpful
  error message.
- `cmd/` subcommands:
  - `alchemist [profile]` — launch TUI with the named (or default) profile.
  - `alchemist profile list` — names + endpoints + which secret source resolves (never
    the secret itself).
  - `alchemist profile add <name> --endpoint ... [--adapter cosmos]` — writes TOML,
    prompts for key → keychain.
  - `alchemist profile set-key <name>` — prompt (masked) → keychain.
  - `alchemist profile remove <name>` — removes TOML entry and keychain item.
- Remove the temporary `--endpoint`/`--key`/`--connection-string` flags from iteration 4
  (keep `--adapter mock` for development).
- Status bar shows the active profile name.

## Out of scope

- AAD / `azidentity` auth (future adapter setting; the design leaves room: profile
  `auth = "key" | "aad"`).
- Saved queries (history covers recall in iteration 7).

## Steps

1. Config load/validate + table-driven tests (temp dirs, `t.Setenv` for XDG).
2. Secret resolver chain + fake keyring tests.
3. Profile subcommands; wire root command to profile → settings map → registry adapter.
4. Docs: README section on profiles and the env fallback for CI/headless machines.

## Testing

**Unit:**
- TOML parse: valid, unknown adapter, missing endpoint, duplicate profile, bad TOML
  (error includes filename).
- Resolution order: keychain hit beats env; env used when keychain errors/misses;
  prompt path returns a typed "needs interaction" signal (prompt itself is tested at the
  TUI layer with a scripted model).
- Name mangling: profile `my-emulator` → `ALCHEMIST_MY_EMULATOR_KEY`.
- `profile add`/`remove` round-trip in a temp config dir with fake keyring; `list`
  output never contains key material.

**Integration:** launch flow resolves the emulator profile end-to-end
(env-var key, real config file in temp XDG dir) and `Ping`s the emulator.

**Manual checklist:**
- [ ] `alchemist profile add emulator --endpoint https://localhost:8081` + key prompt →
      stored in macOS Keychain (verify in Keychain Access).
- [ ] `alchemist` (default profile) connects; status bar shows `emulator`.
- [ ] With keychain item deleted and `ALCHEMIST_EMULATOR_KEY` set, launch still works.
- [ ] `gitleaks` finds nothing after adding a profile.

## Acceptance criteria

- No secret ever written to disk by Alchemist (TOML, logs, history all clean).
- Headless (no keychain) path works via env var with a clear message, not a crash.
- `alchemist profile list` is safe to paste in a bug report.
