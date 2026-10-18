# Everybody Codes CLI

`ec` downloads and decrypts your [Everybody Codes](https://everybody.codes/) quest inputs, organizing them into a directory for each event, quest, and part. It uses only the Go standard library.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/chandlerklein/everybody-codes-cli/cmd/ec@latest
```

This installs `ec` into `GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset (usually `~/go/bin`). Make sure that directory is on your `PATH`.

## Set up your session

Sign in to Everybody Codes, then copy your session cookie value from the profile settings page.

Create `~/.config/everybody-codes/` and save the cookie value in a file named `session` inside it. Include only the raw value, without quotes or the `everybody-codes=` prefix. A trailing newline is fine. Restrict access to the file:

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
ec https://everybody.codes/event/2024/quests/1
```

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

### Choose an output directory

To create the quest under a different directory, use `--dir`:

```sh
ec https://everybody.codes/event/2024/quests/1 --dir ~/projects/everybody-codes
```

This creates `~/projects/everybody-codes/2024/quest-01/`. The flag accepts absolute or relative paths and can appear before or after the URL. Missing directories are created automatically. Without `--dir`, the current directory is used.

Use `ec --help` to see the command syntax.

## Development

From a local checkout, build the binary:

```sh
go build -o ec ./cmd/ec
```

Run the tests:

```sh
go test ./...
```
