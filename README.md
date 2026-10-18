# Everybody Codes CLI

`ec` downloads your [Everybody Codes](https://everybody.codes/) quest inputs and submits your answers from the terminal.

## 1. Install

### Homebrew (macOS)

On Apple Silicon and Intel Macs:

```sh
brew install --cask chandlerklein/tap/ec
ec --version
```

### Download a binary

Download an archive from the [latest release](https://github.com/chandlerklein/everybody-codes-cli/releases/latest). No Go installation is needed.

Choose `darwin` for macOS, `linux` for Linux, or `windows` for Windows. Choose `arm64` for Apple Silicon or other ARM64 systems, or `amd64` for Intel/AMD 64-bit systems.

Extract the archive and put `ec` (`ec.exe` on Windows) in a directory on your `PATH`. Check the installation:

```sh
ec --version
```

### Install with Go

With [Go](https://go.dev/dl/) 1.26 or newer installed, you can also run:

```sh
go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@latest
ec --version
```

If `ec` is not found, add Go's bin directory to your `PATH`. For Bash or Zsh:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

Add that line to `~/.bashrc` or `~/.zshrc` to keep it for new terminals. If you set a custom `GOBIN`, use that path instead.

### Update

For Homebrew installations:

```sh
brew update
brew upgrade --cask chandlerklein/tap/ec
```

For binary downloads and Go installations, run this to install the latest stable release:

```sh
ec update
```

It verifies the download's SHA-256 checksum and replaces your installed executable. It prints `ec is up to date` if you already have the latest version, or `Updated ec from v0.1.0 to v0.2.0` after a successful update. Updates only run when you request them.

The installation directory must be writable. If your version does not recognize `update`, download the latest release or rerun the Go installation command above. Development builds cannot update themselves.

## 2. Sign in

Sign in on the Everybody Codes website and copy your session cookie value from your profile settings. Then run:

```sh
ec auth
```

Paste the value when prompted and press Enter. Use only the value, without quotes or the `everybody-codes=` prefix. It stays hidden while you paste.

Check that it works:

```sh
ec auth check
```

You only need to do this once. If your session expires, run `ec auth` again.

## 3. Download a quest

Create a folder for your solutions and run commands from there:

```sh
mkdir -p everybody-codes
cd everybody-codes
ec get https://everybody.codes/event/2024/quests/1
```

Replace the URL with the quest you want. You can also use the year and quest number:

```sh
ec get -y 2024 -q 1
```

This creates a workspace marker and a folder for each part, and saves inputs for unlocked parts:

```text
everybody-codes/
├── .ec.json
└── 2024/
    └── quest-01/
        ├── part-01/
        │   └── input.txt
        ├── part-02/
        └── part-03/
```

Solve part 1 using `2024/quest-01/part-01/input.txt`.

Keep inputs and quest descriptions private. Add `**/input.txt` to your solutions repository's `.gitignore` before committing.

## 4. Submit an answer

Change into a part directory and submit your answer:

```sh
cd 2024/quest-01/part-01
ec submit 1309
```

Replace `1309` with your answer. Quote answers containing spaces:

```sh
ec submit 'text answer'
```

You can also pipe your program's answer into the CLI:

```sh
./solve | ec submit
```

Your program can use any language. Standard input must be UTF-8 text; `ec` removes one final LF or CRLF and preserves other whitespace. Send program logs to standard error so only the answer is piped into `ec`.

`ec` finds the nearest `.ec.json` in the current directory or its parents and reads the year, quest, and part from your path relative to that workspace. Solver subdirectories such as `part-01/python/` also work. It displays the resolved target with the submission response. Explicit flags override the corresponding inferred values:

```sh
ec submit -p 2 5560
ec submit -y 2024 -q 1 -p 1 1309
```

When your answer is correct, `ec` automatically downloads newly unlocked inputs into the workspace root. Change into `../part-02` for the next part, then `../part-03`, or supply `-p` explicitly.

If you solve a part on the website, run `ec get` from the quest or part directory to download the next input. Existing inputs are kept.

## Workspaces and directory context

There is one `.ec.json` at the workspace root, containing:

```json
{
  "version": 1
}
```

The first successful `ec get` creates it, including when all inputs already exist. A failed download does not create a marker. Existing markers are preserved; malformed files, unsupported versions, symlinks, and other nonregular markers produce an error. `ec submit` and `ec status` never create a marker.

Workspace initialization requires a filesystem that supports hard links, allowing concurrent commands to publish a complete marker without replacing existing configuration.

Without `--dir`, `ec get` downloads into the nearest workspace root, falling back to the current directory when no marker exists. An explicit `--dir` selects that exact destination, even inside another workspace, and creates its marker after a successful download. `ec status` follows the same directory selection and reads local files only.

Automatic puzzle context follows the generated directory layout:

| Directory relative to workspace root | Inferred context |
| --- | --- |
| Workspace root | None |
| `2024/` | Year |
| `2024/quest-01/` | Year and quest |
| `2024/quest-01/part-01/` | Year, quest, and part |
| `2024/quest-01/part-01/python/` | Year, quest, and part |

Event identifiers such as `story-1` also work in place of `2024`. Quest directories must use the generated names (`quest-01`, `quest-10`, and so on); part directories must be `part-01`, `part-02`, or `part-03`. Renaming these directories disables the corresponding inference. Supply any missing quest or part explicitly. The year comes from `-y`, then the workspace path, then the current year. `ec` never guesses the next unsolved part. Running `ec get` without a quest or flags shows help when the current directory supplies no quest.

Existing folders without `.ec.json` continue to work with explicit quest and part flags; their directory names do not supply context, and downloads use the current directory. To enable discovery, run `ec get -y 2024 -q 1` from the intended workspace root, or use `ec get -y 2024 -q 1 --dir /path/to/workspace`. This adds the marker while preserving existing inputs. Run it from the root rather than an individual part directory to establish the correct download destination.

## More commands and options

| What you want to do | Command |
| --- | --- |
| Check which inputs are saved locally | `ec status` |
| Download to a different folder | `ec get -y 2024 -q 1 --dir ~/projects/everybody-codes` |
| Check inputs in that folder | `ec status --dir ~/projects/everybody-codes` |
| Replace existing inputs | `ec get -y 2024 -q 1 --force` |
| See all commands | `ec --help` |
| See a command's options | `ec submit --help` |
| Set up shell completion | `ec completion --help` |

`-y` means year, `-q` means quest, and `-p` means part. Explicit flags override workspace context; a year that cannot be inferred defaults to the current year.

`ec status` uses local files only: `ready` means an input file exists; `locked` means it is missing. It does not check whether you have solved a part.

If you use `--dir` to download elsewhere, change into that workspace or one of its subdirectories before submitting so newly unlocked inputs go to the same place.

Your cookie is saved in `~/.config/everybody-codes/session`. For scripts, you can set `EVERYBODY_CODES_SESSION_COOKIE` instead; a saved cookie takes priority. Inputs are specific to your account, so use separate folders for different accounts.

## Development

The code is organized by responsibility:

- `cmd/ec`: entry point and build version.
- `internal/cli`: command setup, argument validation, and terminal input.
- `internal/everybodycodes`: API requests, quests, answer submission, and local inputs.
- `internal/session`: credential storage and environment fallback.
- `internal/update`: release downloads, checksum verification, and executable replacement.
- `internal/workspace`: workspace marker validation, discovery, and directory context.

Unit tests live with their packages; command integration tests live in `internal/cli`.

Build and test from a checkout:

```sh
go build -o ec ./cmd/ec
go test ./...
```

Pushing a version tag such as `v0.1.0` runs the [release workflow](.github/workflows/release.yml) to publish binaries and checksums.

Stable releases also update the macOS cask in [chandlerklein/homebrew-tap](https://github.com/chandlerklein/homebrew-tap). Prereleases leave the cask unchanged. The release workflow uses the `HOMEBREW_TAP_KEY` secret, an SSH deploy key with write access only to the tap repository. To rotate it, add the new public key as a writable deploy key on the tap and save its private key as that Actions secret in this repository.
