# Everybody Codes CLI

`ec` downloads and decrypts your [Everybody Codes](https://everybody.codes/) quest inputs, submits answers, saves your session cookie, and reports local input readiness.

## Install

### With Go

Requires Go 1.26 or newer. Install the current `main` branch directly from GitHub:

```sh
GOPROXY=direct go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@main
```

Add `GOBIN` to your `PATH`, or `$(go env GOPATH)/bin` if `GOBIN` is unset (usually `~/go/bin`). Check the installation with `ec --version`.

### Release binaries

When published, [GitHub Releases](https://github.com/chandlerklein/everybody-codes-cli/releases) provide binaries that do not require Go:

| System  | Intel/AMD 64-bit                   | ARM64                              |
| ------- | ---------------------------------- | ---------------------------------- |
| macOS   | `ec_<version>_darwin_amd64.tar.gz` | `ec_<version>_darwin_arm64.tar.gz` |
| Linux   | `ec_<version>_linux_amd64.tar.gz`  | `ec_<version>_linux_arm64.tar.gz`  |
| Windows | `ec_<version>_windows_amd64.zip`   | `ec_<version>_windows_arm64.zip`   |

`<version>` omits the tag's `v` prefix. Download the matching archive and `checksums.txt` from the same release, verify its SHA-256 hash, then extract `ec` (`ec.exe` on Windows) into a directory on your `PATH`.

On Linux, verify with `sha256sum --ignore-missing -c checksums.txt`. On macOS, use `shasum -a 256 <archive>`; on Windows, use `Get-FileHash <archive> -Algorithm SHA256`. Compare the latter two results with the archive's entry in `checksums.txt`.

## Authenticate

Sign in to Everybody Codes and copy your session cookie value from your profile settings. Run:

```sh
ec auth
```

Paste the raw value without quotes or the `everybody-codes=` prefix. Terminal input is hidden. The command saves the cookie to `~/.config/everybody-codes/session` with permissions `0600`; it does not verify it. To verify:

```sh
ec auth check
```

This checks your session without downloading inputs or creating quest directories. For an expired or invalid session, run `ec auth` again. Neither command accepts a cookie argument or a configuration flag.

You can instead create the session file manually (restrict it with `chmod 600 ~/.config/everybody-codes/session`) or set:

```sh
export EVERYBODY_CODES_SESSION_COOKIE='your-cookie-value'
```

For Fish, use `set -gx EVERYBODY_CODES_SESSION_COOKIE 'your-cookie-value'`.

The session file takes precedence; a missing, empty, or whitespace-only file falls back to the environment variable. Surrounding whitespace is trimmed. An unreadable or invalid nonempty file produces an error. Cookie values are not logged.

## Download inputs

> [!CAUTION]
> Do not commit quest inputs or descriptions. Get permission before reusing or sharing them. Add `**/input/` to your repository's `.gitignore` to exclude generated input directories.

Run from your solutions directory using a quest URL or year and quest flags:

```sh
ec get https://everybody.codes/event/2024/quests/1
ec get -y 2024 -q 1
ec get --year 2024 --quest 01
```

These commands select the same quest. `-y` and `-q` are aliases for `--year` and `--quest`; do not combine them with a URL. Omit `--year` to use the current year, for example `ec get -q 1`. Quest numbers are positive decimal integers, so `01` means 1 and `08` means 8.

Each command creates:

```text
2024/quest-01/
├── part-01/
│   └── input/
├── part-02/
│   └── input/
└── part-03/
    └── input/
```

Each part gets an `input/` directory containing `input-encoded.txt` with the encrypted input, plus `input.txt` when its decryption key is available. Quest numbers are padded to at least two digits. `get` no longer creates `answer.txt`; any existing answer files are left untouched, including with `--force`.

Both forms accept `--dir` before or after the quest arguments:

```sh
ec get https://everybody.codes/event/2024/quests/1 --dir ~/projects/everybody-codes
```

This writes under `~/projects/everybody-codes/2024/quest-01/`. The default is the current directory. Relative and absolute paths are supported; missing directories are created.

Rerun after unlocking another part. Existing `input.txt` files are preserved and skipped. Pending parts reuse usable encrypted files and request fresh keys; missing or malformed encrypted files are downloaded again. When all three decoded files exist, no session or HTTP requests are needed.

Use `--force` (or `-f`) to download fresh encrypted inputs and overwrite existing input files:

```sh
ec get -y 2024 -q 1 --force
```

Existing files and directories are reused by default. Inputs are account-specific: use separate output directories for different accounts. Changing the session does not replace cached or decoded files unless you pass `--force`. If the server reports an unavailable quest, the error identifies the year and quest; the server may use the same response for missing events, missing quests, and inputs not yet available.

## Submit answers

Pass the answer as one argument or pipe your program's output:

```sh
ec submit -y 2024 -q 1 -p 1 1309
ec submit --year 2024 --quest 01 --part 1 'text answer'
./solve | ec submit -y 2024 -q 1 -p 1
```

`--quest`/`-q` and `--part`/`-p` are required positive decimal integers. Quest numbers may have leading zeros; part numbers must not (`1`, not `01`). `--year`/`-y` defaults to the current year, as with `get`.

Answers are UTF-8 text, including numeric answers. Quote answers containing spaces; use `--` before an answer starting with a hyphen. If the argument is omitted, `submit` reads standard input until EOF and removes one final LF or CRLF, preserving other whitespace and line breaks. An explicit argument takes precedence over standard input. Empty or whitespace-only answers and invalid UTF-8 are rejected before any request.

`submit` uses the same saved session or environment variable as `get`. It posts `{"answer":"<ANSWER>"}` to `https://api.everybody.codes/event/<YEAR>/quest/<QUEST>/part/<PART>/answer`. It does not require local quest files or create any files.

For a 2xx response, the command prints the HTTP status and response body. Other responses exit with an error containing the HTTP status and response body, including unknown statuses. HTTP 409 Conflict reports that the part is already solved. Requests are not retried automatically; the API's response body provides the result of the submission.

## Show local status

Run from the directory containing your year folders, or pass `--dir`:

```sh
ec status
ec status --dir ~/projects/everybody-codes
```

```text
2024
  quest-01
    part-01: ready
    part-02: locked
    part-03: locked
```

Each local quest lists all three parts. Status checks the same zero-padded directories as `get`: `part-01`, `part-02`, and `part-03`. Other spellings, such as `part-1` or `part-001`, are ignored. A part is `ready` if `input/input.txt` is a regular file; otherwise it is `locked`, including missing part directories. Status uses local files only and does not track solved parts or remote availability. An empty directory reports `No local quests found.`; a missing base directory is an error.

## Help and shell completion

Run `ec` or `ec --help` for available commands. Each command supports `--help`, including `ec get --help`, `ec submit --help`, `ec auth check --help`, and `ec status --help`.

Use `ec completion --help` for Bash, Zsh, Fish, and PowerShell completion setup, then `ec completion <shell> --help` for installation instructions.

## Development

Build and test from a checkout:

```sh
go build -o ec ./cmd/ec
go test ./...
```

`ec --version` reports the release tag for release binaries and the embedded module version for versioned Go installs. Local builds otherwise report `dev`.

### Releases

After merging changes to `main`, tag the commit to release with a new version:

```sh
git switch main
git pull --ff-only origin main
git tag v0.1.0
git push origin v0.1.0
```

The [release workflow](.github/workflows/release.yml) runs tests, then uses [GoReleaser](https://goreleaser.com/) to publish the six archives above and `checksums.txt`. Tags such as `v0.2.0-rc.1` create prereleases.

With GoReleaser v2 installed, validate and build locally without publishing:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Artifacts go to the ignored `dist/` directory.
