# hasher

A small CLI on top of [`github.com/cccteam/ccc/securehash`](../..) for generating
and verifying password hashes (bcrypt or argon2id) at the command line.

## Install

As a Go tool inside a consumer module (Go 1.24+):

```sh
go get -tool github.com/cccteam/ccc/securehash/cmd/hasher@latest
go tool hasher --help
```

Or as a standalone binary on `$PATH`:

```sh
go install github.com/cccteam/ccc/securehash/cmd/hasher@latest
hasher --help
```

## Quick start

```sh
# Hash a password (interactive, hidden TTY prompt with confirmation)
hasher hash

# Pipe stdin
printf '%s' 'hunter2' | hasher hash --algo bcrypt

# Verify
HASH=$(printf '%s' 'hunter2' | hasher hash --algo argon2)
echo "$HASH" | hasher verify --algo argon2 --password 'hunter2'
```

## Configuration

Flags are bound to viper. In precedence order: flags > env > config file > defaults.

| Setting    | Flag       | Env                | Config key  | Default  |
| ---------- | ---------- | ------------------ | ----------- | -------- |
| Algorithm  | `--algo`   | `HASHER_ALGORITHM` | `algorithm` | `argon2` |
| Output     | `--output` | `HASHER_OUTPUT`    | `output`    | `text`   |
| Config     | `--config` | `HASHER_CONFIG`    | —           | —        |

Config file lookup (first match wins):

- `$XDG_CONFIG_HOME/hasher/hasher.yaml`
- `$HOME/.config/hasher/hasher.yaml`
- `./hasher.yaml`

Example config:

```yaml
algorithm: bcrypt
output: json
```

## Tab completion

```sh
# Auto-detect $SHELL and install
hasher completion install

# Explicit shell, write to stdout instead
hasher completion install --shell zsh --print > _hasher
```

Cobra's raw `hasher completion <bash|zsh|fish|powershell>` is also available
for piping into custom locations.

## Man pages

A pre-generated `man/hasher.1` is committed to this directory for packagers.
You can also generate or install on demand:

```sh
hasher man generate ./out          # write into ./out
hasher man install                 # write to $XDG_DATA_HOME/man/man1
sudo hasher man install --system   # write to /usr/local/share/man/man1
```

## Exit codes

| Code | Meaning                                               |
| ---- | ----------------------------------------------------- |
| 0    | success                                               |
| 1    | `verify` mismatch, or general failure                 |
| 2    | `verify` error (malformed hash, unreadable input, …)  |

## Notes

- Only the library's recommended default parameters are exposed; the CLI does
  not let you tune bcrypt cost or argon2 memory/time/parallelism. If you need
  custom parameters, drive `securehash` directly from Go.
- `--password` is convenient but is recorded in shell history and visible in
  the process list. Prefer the TTY prompt or piped stdin.
