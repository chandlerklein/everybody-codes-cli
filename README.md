# Everybody Codes CLI

`ec` downloads and decrypts your [Everybody Codes](https://everybody.codes/) quest inputs, organizing them into a directory for each event, quest, and part.

## Install

### Prebuilt binaries

Download an archive for your system from [GitHub Releases](https://github.com/chandlerklein/everybody-codes-cli/releases/latest). No Go installation is required.

| System | Intel/AMD 64-bit (`amd64`) | ARM64 (`arm64`) |
| --- | --- | --- |
| macOS | `ec_<version>_darwin_amd64.tar.gz` | `ec_<version>_darwin_arm64.tar.gz` (Apple silicon) |
| Linux | `ec_<version>_linux_amd64.tar.gz` | `ec_<version>_linux_arm64.tar.gz` |
| Windows | `ec_<version>_windows_amd64.zip` | `ec_<version>_windows_arm64.zip` |

Archive filenames use the version without the `v` prefix, such as `ec_0.1.0_linux_amd64.tar.gz`. Download `checksums.txt` from the same release to verify the archive's SHA-256 hash before extracting it.

For example, for Linux on Intel/AMD, run from the download directory:

```sh
sha256sum --ignore-missing -c checksums.txt
tar -xzf ec_0.1.0_linux_amd64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 ec "$HOME/.local/bin/ec"
```

Use the filename for your downloaded version. On macOS, verify with `shasum -a 256 ec_0.1.0_darwin_arm64.tar.gz` (or the `amd64` archive) and compare the result with its entry in `checksums.txt`, then extract and install as above. Make sure `~/.local/bin` is on your `PATH`.

On Windows, use PowerShell's `Get-FileHash .\ec_0.1.0_windows_amd64.zip -Algorithm SHA256` (or the `arm64` archive), compare it with `checksums.txt`, and extract the ZIP. Move `ec.exe` into a directory on your `PATH`.

After installing:

```sh
ec --version
```

### Install with Go

Requires Go 1.26 or newer.

```sh
go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@latest
```

To install a specific published version:

```sh
go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@v0.1.0
```

This installs `ec` into `GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset (usually `~/go/bin`). Make sure that directory is on your `PATH`.

`ec --version` reports the installed version for both release downloads and versioned Go installs. Local builds use Go's embedded module version when available and otherwise report `dev`.

## Set up your session

Sign in to Everybody Codes, then copy your session cookie value from the profile settings page.

Run:

```sh
ec auth
```

Paste the raw cookie value at the prompt, without quotes or the `everybody-codes=` prefix. Input is hidden when using a terminal. The command saves it to `~/.config/everybody-codes/session` with permissions `0600`. It accepts no cookie argument or configuration flags.

To verify your session:

```sh
ec auth check
```

This checks the authenticated user endpoint without downloading inputs or creating quest directories. If authentication fails, run `ec auth` again to replace the cookie.

You can also create `~/.config/everybody-codes/` and save the raw cookie value in a file named `session` inside it. A trailing newline is fine. Restrict access to the file:

```sh
chmod 600 ~/.config/everybody-codes/session
```

Alternatively, set an environment variable:

```sh
export EVERYBODY_CODES_SESSION_COOKIE='your-cookie-value'
```

In Fish:

```fish
set -gx EVERYBODY_CODES_SESSION_COOKIE 'your-cookie-value'
```

The session file takes precedence. If it is missing, empty, or contains only whitespace, `ec` uses the environment variable. If neither has a value, `ec` reports an error. The cookie value is never logged.

## Download quest inputs

> [!CAUTION]
> Do not commit quest inputs or descriptions to your code repository. Get permission before reusing or sharing this content. Add the following to your repository's `.gitignore` to exclude all generated input directories:
>
> ```gitignore
> **/input/
> ```

Run from the directory where you keep your quest solutions:

```sh
ec get https://everybody.codes/event/2024/quests/1
```

Or specify the event and quest directly:

```sh
ec get -e 2024 -q 1
ec get --event 2024 --quest 01
```

Both forms download the same inputs as the URL above. `-e` is short for `--event`, and `-q` is short for `--quest`. Supply both flags together, without a quest URL. Quest numbers are decimal and may have leading zeros, so `01` means quest 1 and `08` means quest 8. Both forms also support `--dir`.

If the server reports a missing event or quest, `ec` exits with an error identifying the requested event and quest. The server does not distinguish missing events from missing quests; inputs that are not available yet can produce the same response.

This creates:

```text
2024/
└── quest-01/
    ├── part-01/input/
    ├── part-02/input/
    └── part-03/input/
```

Each part's `input/` directory receives:

| File | Contents |
| --- | --- |
| `input-encoded.txt` | Encrypted quest input. |
| `input.txt` | Decrypted quest input, saved when the part's key is available. |

Quest numbers are padded to at least two digits: quest 1 uses `quest-01`, and quest 12 uses `quest-12`.

Run the same command again after unlocking another part to download and decrypt its input. Existing directories are reused, and parts that already have `input.txt` are skipped. Once all three parts have `input.txt`, the command finishes without making HTTP requests or requiring a session cookie.

For pending parts, saved encrypted inputs are reused. If all pending parts have usable `input-encoded.txt` files, a rerun requests only the quest keys. Missing or malformed encrypted inputs are downloaded again.

Inputs are specific to your account. Use separate output directories for different accounts; changing the session cookie does not replace cached inputs or existing decoded files.

### Choose an output directory

To create the quest under a different directory, use `--dir`:

```sh
ec get https://everybody.codes/event/2024/quests/1 --dir ~/projects/everybody-codes
```

This creates `~/projects/everybody-codes/2024/quest-01/`. The flag accepts absolute or relative paths and can appear before or after the URL. Missing directories are created automatically. Without `--dir`, the current directory is used.

Run `ec` by itself or use `ec --help` to see the available commands. Use `ec get --help` for download syntax. Each command also has its own help, such as `ec auth --help`, `ec auth check --help`, and `ec status --help`.

## Show local quest status

Run from the directory containing your event folders:

```sh
ec status
ec status --dir ~/projects/everybody-codes
```

Parts are grouped by event and then quest. All three parts are listed for each local quest:

```text
2024
  quest-01
    part-01: ready
    part-02: locked
    part-03: locked
```

Each part is `ready` when its decoded `input.txt` file exists, or `locked` when it does not. Missing part directories are also shown as locked. Status uses local files only and does not indicate whether a part has been solved or whether more parts are available remotely.

## Development

From a local checkout, build the binary:

```sh
go build -o ec ./cmd/ec
```

Run the tests:

```sh
go test ./...
```

### Releases

After merging the release changes to `main`, create and push a version tag on the commit to release:

```sh
git switch main
git pull --ff-only origin main
git tag v0.1.0
git push origin v0.1.0
```

Use a new version for each release. The [release workflow](.github/workflows/release.yml) runs tests first, then uses [GoReleaser](https://goreleaser.com/getting-started/quick-start/) to build `./cmd/ec` for macOS, Linux, and Windows on `amd64` and `arm64`. It publishes six archives and `checksums.txt` to GitHub Releases using the workflow's built-in `GITHUB_TOKEN`. Tags with a prerelease suffix, such as `v0.2.0-rc.1`, create GitHub prereleases.

With GoReleaser v2 installed, validate the configuration and build the release archives locally without publishing:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

Generated binaries, archives, and checksums are written to the ignored `dist/` directory.
