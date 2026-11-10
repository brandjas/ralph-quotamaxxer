# quotamaxxer

## Build & Test

The Go module is in `cmd/quotamaxxer/` (not the repo root):

```sh
cd cmd/quotamaxxer
go build -o ../../quotamaxxer .
go test ./...
```

Shell tests of the built binary use bats (`bats test/`, from the repo root).
End-to-end tests with real Claude Code run in Docker containers on musl and
glibc images: `test/e2e/run.sh` (`--tier fake` needs no credentials or network).

## Install

`./install.sh` builds from source and installs to `~/.claude/ralph-quotamaxxer/bin/`.
