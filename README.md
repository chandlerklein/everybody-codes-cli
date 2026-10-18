# Everybody Codes CLI

`ec` downloads your [Everybody Codes](https://everybody.codes/) quest inputs and submits your answers from the terminal.

## 1. Install

With [Go](https://go.dev/dl/) 1.26 or newer installed, run:

```sh
GOPROXY=direct go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@main
ec --version
```

If `ec` is not found, add Go's bin directory to your `PATH`. For Bash or Zsh:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

Add that line to `~/.bashrc` or `~/.zshrc` to keep it for new terminals. If you set a custom `GOBIN`, use that path instead.

Prefer a download? When available, [GitHub Releases](https://github.com/chandlerklein/everybody-codes-cli/releases) has binaries for macOS, Linux, and Windows. Extract the archive for your system and put `ec` (`ec.exe` on Windows) in a directory on your `PATH`.

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

This creates a folder for each part and saves inputs for unlocked parts:

```text
2024/quest-01/
├── part-01/
│   └── input.txt
├── part-02/
└── part-03/
```

Solve part 1 using `2024/quest-01/part-01/input.txt`.

Keep inputs and quest descriptions private. Add `**/input.txt` to your solutions repository's `.gitignore` before committing.

## 4. Submit an answer

From the same solutions folder, submit your answer with the year, quest, and part:

```sh
ec submit -y 2024 -q 1 -p 1 1309
```

Replace `1309` with your answer. Quote answers containing spaces:

```sh
ec submit -y 2024 -q 1 -p 1 'text answer'
```

You can also pipe your program's answer into the CLI:

```sh
./solve | ec submit -y 2024 -q 1 -p 1
```

When your answer is correct, `ec` automatically downloads newly unlocked inputs into the current folder. Continue with `-p 2`, then `-p 3`.

If you solve a part on the website, run `ec get` for that quest again to download the next input. Existing inputs are kept.

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

`-y` means year, `-q` means quest, and `-p` means part. Omit `-y` to use the current year.

`ec status` uses local files only: `ready` means an input file exists; `locked` means it is missing. It does not check whether you have solved a part.

If you use `--dir` to download elsewhere, change into that folder before submitting so newly unlocked inputs go to the same place.

Your cookie is saved in `~/.config/everybody-codes/session`. For scripts, you can set `EVERYBODY_CODES_SESSION_COOKIE` instead; a saved cookie takes priority. Inputs are specific to your account, so use separate folders for different accounts.

## Development

`cmd/ec` contains the entry point. `internal/cli` contains the commands, supporting code, and their tests.

Build and test from a checkout:

```sh
go build -o ec ./cmd/ec
go test ./...
```

Pushing a version tag such as `v0.1.0` runs the [release workflow](.github/workflows/release.yml) to publish binaries and checksums.
