# ec

A Go CLI for downloading Everybody Codes quest inputs. Uses only the Go standard library. Requires Go 1.27.2 or newer.

## Build

```sh
go build -o ec ./cmd/ec
```

Or install `ec` into your Go binary directory:

```sh
go install ./cmd/ec
```

With default Go settings, this installs `~/go/bin/ec`. Ensure `~/go/bin` is on your `PATH` to invoke it as `ec`.

## Session

Save your raw `everybody-codes` cookie value in `~/.config/everybody-codes/session`. The file should contain only the value, without quotes or a `everybody-codes=` prefix. A trailing newline is fine.

```sh
chmod 600 ~/.config/everybody-codes/session
```

The CLI reads this file directly and does not log the cookie. It does not require an environment variable.

## Initialize a quest

Run from the directory where you keep your quest solutions:

```sh
ec init https://everybody.codes/event/2024/quests/1
```

This creates:

```text
2024/quest-01/
  part-01/input/
  part-02/input/
  part-03/input/
```

Each part receives `notes-encoded.txt`. Parts with available AES keys also receive decoded `notes.txt`. Quest numbers have a minimum width of two digits, so quest 12 uses `quest-12`.

Existing directories are reused. Parts that already have `notes.txt` are skipped, preserving their files. Run the command again after unlocking another part to decode its notes. Once all three parts have `notes.txt`, the command finishes without making HTTP requests.

## Tests

```sh
go test ./...
```
