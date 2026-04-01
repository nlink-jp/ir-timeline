# AGENTS.md — ir-timeline

## Summary

IR timeline recorder — single-binary, browser-based tool for tracking incident response events with text, images, tags, and time deltas. Go + SQLite (pure Go via `modernc.org/sqlite`, no CGO).

## Module

```
github.com/nlink-jp/ir-timeline
```

## Build & Test

```bash
make build       # → dist/ir-timeline
make build-all   # → dist/ir-timeline-{os}-{arch} (5 platforms)
make test        # → go test ./... -v
make check       # → test + build
make clean       # → rm -rf dist/
```

## Key Files

```
main.go          — entry point, CLI flags, HTTP server, graceful shutdown, import subcommand
storage.go       — SQLite schema, migration, CRUD (meta, events, event_tags, event_images)
handler.go       — HTTP handlers (REST API + static file serving)
import.go        — JSON/CSV file import logic
web/index.html   — SPA (HTML + CSS + JS, embedded via embed.FS)
docs/design.md   — design document
```

## Architecture

- Single `package main` — all Go files at project root
- `embed.FS` embeds `web/*` into the binary
- SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- All timestamps stored as UTC in DB; displayed in incident timezone via frontend
- `event_tags` table for many-to-many tag support
- `event_images` stores image BLOBs directly in SQLite

## Gotchas

- `modernc.org/sqlite` does not support CGO — `CGO_ENABLED=0` is safe and required for cross-compile
- Timestamps in DB are UTC-normalized; `input_tz` column records the original input timezone
- `ListEvents` sorts in Go (not SQL) via `parseTimestamp` to handle mixed TZ formats in legacy data
- Incremental DB migrations use `addColumnIfNotExists` pattern (no migration framework)
- The `import` subcommand is detected before `flag.Parse`; args are reordered to allow `ir-timeline import file.json --db x.db`
