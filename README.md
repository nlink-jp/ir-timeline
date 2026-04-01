# ir-timeline

Incident response timeline recorder — a single-binary, browser-based tool for tracking IR events with text, images, tags, and time deltas.

[日本語版 README はこちら](README.ja.md)

## Concept

Replace Excel-based IR timeline management with a modern, local-first tool:

- **Single binary** — no runtime dependencies, no Python, no Node.js
- **Single DB file** — one SQLite file per incident, images stored as BLOBs
- **Browser UI** — modern timeline view with dark/light theme
- **Portable** — copy the DB file to share or archive the incident

## Quick Start

```bash
# Build
make build

# Run (creates timeline.db if not exists, opens browser)
./dist/ir-timeline

# Use a specific DB file
./dist/ir-timeline --db incident-2026-04-01.db

# Custom port, no auto-open
./dist/ir-timeline --port 9090 --no-browser
```

## Features

### List View (vertical timeline)

Events displayed chronologically with cards showing description, actor, tags, images, and time deltas between events.

### Chart View (horizontal swimlane)

Time on the X-axis, tags as swimlanes on the Y-axis. Events with multiple tags appear in all relevant lanes. Hover for details, click to edit.

### Common Features

| Feature | Detail |
|---------|--------|
| **Multiple tags** | Each event can have multiple tags for flexible grouping |
| **Tag colors** | Auto-assigned colors (fixed for common IR phases, hash-based for custom) |
| **Image attach** | Drag & drop or file picker, stored in SQLite as BLOBs |
| **Image preview** | Thumbnails in cards, click for lightbox |
| **Time delta** | Elapsed time between consecutive events |
| **Dark / Light** | Theme toggle, preference saved in localStorage |
| **Case ID** | Badge display in header, editable |
| **Tag filter** | Filter timeline by specific tag |
| **Markdown export** | Download timeline as `.md` file |

## CLI Flags

```
ir-timeline [flags]

  --db <path>       SQLite database path (default: timeline.db)
  --port <number>   HTTP server port (default: 8888)
  --no-browser      Don't auto-open browser
  --version         Show version
```

## Predefined Tag Colors

| Tag | Color |
|-----|-------|
| `detection` | Blue |
| `analysis` | Purple |
| `containment` | Orange |
| `eradication` | Red |
| `recovery` | Green |
| `communication` | Teal |
| `lesson` | Indigo |

Any other tag name gets an auto-generated color based on its hash.

## Security

- Binds to `127.0.0.1` only — no remote access
- All SQL queries use parameterized statements
- DOM-based rendering (`textContent` / `createElement`), no `innerHTML`
- Image upload limited to 10 MB, `image/*` MIME types only

## Architecture

```
ir-timeline (Go binary)
├── main.go         — entry point, flags, HTTP server
├── storage.go      — SQLite schema, CRUD operations
├── handler.go      — REST API handlers
└── web/
    └── index.html  — SPA (HTML + CSS + JS, embedded via embed.FS)
```

See [docs/design.md](docs/design.md) for full design document.

## Build

```bash
make build          # → dist/ir-timeline
make test           # → go test ./...
make build-all      # → 5 platform binaries
make clean          # → remove dist/
```

## Part of cybersecurity-series

ir-timeline is part of the [cybersecurity-series](https://github.com/nlink-jp/cybersecurity-series) —
AI-augmented tools for threat intelligence, incident response, and security operations.
