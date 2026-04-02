# quotamaxxer

## Build & Test

The Go module is in `cmd/quotamaxxer/` (not the repo root):

```sh
cd cmd/quotamaxxer
go build -o ../../quotamaxxer .
go test ./...
```

## Install

`./install.sh` builds from source and installs to `~/.claude/ralph-quotamaxxer/bin/`.
