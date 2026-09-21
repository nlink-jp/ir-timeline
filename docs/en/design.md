# ir-timeline Design Document

## 1. Overview

A tool for recording and managing the timeline of an IR (incident response)
engagement. It replaces timeline management in Excel and provides event
recording, image attachment, tag classification and elapsed-time display in a
modern browser-based UI.

### Design Goals

| Goal | Detail |
|------|--------|
| **Single binary** | Built with Go, no CGO required (`modernc.org/sqlite`) |
| **Single DB file** | One incident = one SQLite file. Images are stored inside the DB as BLOBs too |
| **Zero config** | Runs from the binary alone. No dependency on external services |
| **Modern UI** | Browser-based SPA, dark/light theme, Japanese/English i18n |
| **Portable** | Copying the DB file is all it takes to carry an incident to another PC |

### Non-Goals

- Multi-user concurrent editing
- Automatic analysis by an LLM (→ ir-tracker's job)
- Remote server deployment

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────┐
│                    ir-timeline                        │
│                   (Go binary)                        │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────┐ │
│  │ main.go  │→ │handler.go│→ │    storage.go       │ │
│  │ (flags,  │  │(HTTP API)│  │ (SQLite CRUD,       │ │
│  │  server,  │  └──────────┘  │  UTC normalization, │ │
│  │  signal,  │       ↑        │  TZ conversion)     │ │
│  │  tz)     │       │        └─────────┬──────────┘ │
│  └──────────┘       │                  │            │
│       │      ┌───────────────┐  ┌──────────┐       │
│       │      │  embed.FS     │  │ .db file │       │
│       │      │ (web/*)       │  │ (SQLite) │       │
│       │      └───────────────┘  └──────────┘       │
│       │                                              │
│  ┌──────────┐                                        │
│  │import.go │ ← CLI import subcommand (JSON/CSV)     │
│  └──────────┘                                        │
└──────────────────────────────────────────────────────┘
         ↕ HTTP (localhost only)
┌──────────────────────────────────────────────────────┐
│                Web Browser (SPA)                      │
│  index.html + inline CSS/JS                           │
│  - List View (vertical timeline)                      │
│  - Chart View (horizontal swimlane, zoom/pan)         │
│  - Event CRUD (modal forms, per-event TZ selector)    │
│  - Image upload / preview / lightbox                  │
│  - Multi-tag filter (checkbox dropdown)               │
│  - Markdown export                                    │
│  - i18n (en/ja), dark/light theme                     │
└──────────────────────────────────────────────────────┘
```

### File Structure

```
ir-timeline/
├── main.go            # Entry point, flags, HTTP server, graceful shutdown,
│                      #   timezone resolution, import subcommand dispatch
├── storage.go         # SQLite schema, incremental migration, CRUD,
│                      #   UTC normalization, TZ-aware export
├── handler.go         # HTTP handlers (REST API + static file serving + timezone)
├── import.go          # JSON/CSV file import logic
├── storage_test.go    # Storage layer tests
├── handler_test.go    # HTTP handler tests
├── import_test.go     # Import logic tests
├── web/
│   └── index.html     # SPA (HTML + embedded CSS + JS)
├── docs/
│   ├── en/
│   │   └── design.md      # This document (English)
│   └── ja/
│       └── design.ja.md   # This document (Japanese)
├── Makefile           # build, build-all, test, check, clean
├── AGENTS.md          # Project summary for AI agents
├── go.mod
├── go.sum
├── .gitignore
├── LICENSE
├── CHANGELOG.md
├── README.md
└── README.ja.md
```

---

## 3. Data Model

One incident = one SQLite file. There are four tables.

### Timestamp storage rules

- **Every timestamp in the DB is stored in UTC (ISO 8601, `Z` suffix)**
- The TZ the event was entered in is recorded in the `events.input_tz` column (the original zone can be restored)
- Normalized with `toUTC()` on save, converted to the incident TZ on display
- SQLite's `ORDER BY timestamp ASC` is therefore already the correct chronological order

### 3.1 `meta` — incident metadata

A KV store. Holds the title, the case ID, the timezone and so on.

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT PK | Key name |
| `value` | TEXT NOT NULL | Value |

**Keys:**

| Key | Example Value | Description |
|-----|---------------|-------------|
| `title` | `"2026-04-01 フィッシング対応"` | Incident title |
| `case_id` | `"INC-2026-0042"` | Case ID (ticket number etc., optional) |
| `timezone` | `"Asia/Tokyo"` | Timezone used for display (IANA) |
| `created_at` | `"2026-04-01T05:00:00Z"` | Creation time (UTC) |

### 3.2 `events` — timeline events

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK AUTOINCREMENT | Event ID |
| `timestamp` | TEXT NOT NULL | When the event occurred (UTC, ISO 8601) |
| `timestamp_end` | TEXT | End time (UTC, optional — for range events) |
| `input_tz` | TEXT NOT NULL DEFAULT 'UTC' | Timezone the event was entered in (IANA) |
| `description` | TEXT NOT NULL DEFAULT '' | What happened (free text) |
| `actor` | TEXT NOT NULL DEFAULT '' | Responder or team name |
| `created_at` | TEXT NOT NULL | Record creation time |
| `updated_at` | TEXT NOT NULL | Record update time |

**Indexes:** `idx_events_timestamp ON events(timestamp)`

### 3.3 `event_tags` — event tags (many-to-many)

An event can carry several tags. The tags are what group the swimlanes.

| Column | Type | Description |
|--------|------|-------------|
| `event_id` | INTEGER NOT NULL FK→events(id) ON DELETE CASCADE | Event ID |
| `tag` | TEXT NOT NULL | Tag name (e.g. detection, containment) |

**PK:** `(event_id, tag)` — the same tag cannot appear twice on one event

**Indexes:** `idx_event_tags_tag ON event_tags(tag)`

**Note:** in the Chart View's swimlanes, an event with several tags has a
marker drawn in every lane it belongs to.

### 3.4 `event_images` — images attached to an event

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK AUTOINCREMENT | Image ID |
| `event_id` | INTEGER NOT NULL FK→events(id) ON DELETE CASCADE | Owning event |
| `filename` | TEXT NOT NULL | Original file name |
| `content_type` | TEXT NOT NULL | MIME type (image/png, image/jpeg etc.) |
| `data` | BLOB NOT NULL | Image binary |
| `created_at` | TEXT NOT NULL | Upload time |

**Indexes:** `idx_event_images_event_id ON event_images(event_id)`

### ER Diagram

```
meta [key PK, value]

events [id PK, timestamp(UTC), timestamp_end(UTC), input_tz, description, actor, ...]
  │
  ├─ 1:N ─→ event_tags [event_id FK + tag PK]
  │
  └─ 1:N ─→ event_images [id PK, event_id FK, filename, content_type, data, created_at]
```

### Incremental Migration

A new column is migrated into an existing DB automatically, through the
`addColumnIfNotExists` pattern:
- `timestamp_end TEXT` (v0.1.0)
- `input_tz TEXT NOT NULL DEFAULT 'UTC'` (v0.1.0)

---

## 4. API Design

Base URL: `http://127.0.0.1:{port}`

### 4.1 Static Files

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Returns the SPA (index.html) |

### 4.2 Meta API

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| GET | `/api/meta` | — | `{"title": "...", "case_id": "...", "timezone": "...", ...}` |
| PUT | `/api/meta` | `{"title": "...", "case_id": "...", "timezone": "..."}` | `{"ok": true}` |

### 4.3 Timezone API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/timezone` | `{"timezone": "Asia/Tokyo"}` |

Resolved in priority order: the `--tz` CLI flag > DB meta > the system local zone.

### 4.4 Events API

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| GET | `/api/events` | — | `[{event}, ...]` sorted by timestamp (UTC) |
| POST | `/api/events` | `{timestamp, timestamp_end, input_tz, description, actor, tags}` | `{event}` |
| PUT | `/api/events/:id` | `{timestamp, timestamp_end, input_tz, description, actor, tags}` | `{event}` |
| DELETE | `/api/events/:id` | — | `{"ok": true}` |

**Event JSON shape:**

```json
{
  "id": 1,
  "timestamp": "2026-04-01T05:00:00Z",
  "timestamp_end": null,
  "input_tz": "Asia/Tokyo",
  "description": "ユーザーから不審メール報告",
  "actor": "SOC Team",
  "tags": ["detection", "communication"],
  "created_at": "2026-04-01 14:05:00",
  "updated_at": "2026-04-01 14:05:00",
  "images": [
    {"id": 1, "event_id": 1, "filename": "screenshot.png", "content_type": "image/png"}
  ]
}
```

### 4.5 Images API

| Method | Path | Request | Response |
|--------|------|---------|----------|
| POST | `/api/events/:id/images` | multipart/form-data (`file` field) | `{image}` |
| GET | `/api/images/:id` | — | image binary (Content-Type set) |
| DELETE | `/api/images/:id` | — | `{"ok": true}` |

**Constraints:** an image must be 10MB or smaller and only the `image/*` MIME type is allowed. The Content-Type is validated against both the header and the magic bytes.

### 4.6 Tags API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/tags` | `["detection", "analysis", "containment", ...]` |

Returns the list of tags in use (`SELECT DISTINCT tag FROM event_tags`).

### 4.7 Export API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/export/markdown` | A `text/markdown` file download (timezone already converted) |

---

## 5. UI Design

### 5.1 View Modes

A toggle in the toolbar switches between two display modes.

#### 5.1.1 List View (vertical timeline) — the default

Events are shown in chronological order from top to bottom, as cards that
carry the details and the images. A range event shows its start, its end and
its duration in the form "14:00 — 15:30 (1h30m)".

#### 5.1.2 Chart View (horizontal timeline / swimlanes)

A swimlane view with time on the horizontal axis and tags (the groups)
dividing the vertical axis. It gives an overview of how the whole incident
progressed in time and of which phases ran in parallel.

**What the Chart View offers:**

| Feature | Detail |
|---------|--------|
| **Swimlane** | One horizontal lane per tag. An untagged event goes in the "(untagged)" lane |
| **Point/Range** | A point event is drawn as a dot, a range event as a rounded bar |
| **Time Axis** | Auto-scaling, with 5% padding at each end |
| **Cursor Bar** | A vertical line plus a date-time label floats at the mouse position |
| **Zoom** | The mouse wheel zooms around the cursor position (up to 50x). There are +/− buttons as well |
| **Pan** | Drag to pan the time axis left and right |
| **Hover Popup** | Hovering a marker shows a summary (time, description, Actor) as a tooltip |
| **Click Detail** | A click opens the edit modal (the same modal the List View uses) |
| **Clip Path** | Markers are clipped so none spills outside the drawing area |

### 5.2 Common Features (shared by both views)

| Feature | Detail |
|---------|--------|
| **Sticky Header** | The header and toolbar are `position: sticky`, so they stay put while the page scrolls |
| **View Toggle** | The [List / Chart] buttons switch views. The choice is saved in localStorage |
| **Time Delta** | List View: the elapsed time is shown between events. Chart View: the axis ticks plus the cursor bar |
| **Multi-Tag Filter** | A checkbox dropdown selects several tags (an OR filter). Each entry carries the tag's color dot |
| **Image Attach** | Images are attached to an event by drag and drop or by choosing a file |
| **Image Preview** | Thumbnails; a click opens an enlarged lightbox |
| **Inline Edit** | Clicking an event card or a marker opens the edit modal |
| **Dark/Light** | Theme switch. Saved in localStorage |
| **i18n** | Japanese/English switch. Saved in localStorage. Covers every UI string |
| **Markdown Export** | One button downloads the timeline as a Markdown file (TZ already converted) |

### 5.3 Tag Color Mapping

An HSL color is generated automatically from the hash of the tag name. The IR
tags in common use are given a fixed color:

| Tag | Color |
|-----|-------|
| `detection` | Blue (#3b82f6) |
| `analysis` | Purple (#8b5cf6) |
| `containment` | Orange (#f59e0b) |
| `eradication` | Red (#ef4444) |
| `recovery` | Green (#10b981) |
| `communication` | Teal (#14b8a6) |
| `lesson` | Indigo (#6366f1) |
| (other) | Hash-based HSL |

### 5.4 Modal Form

The modal dialog for adding and editing an event:

| Field | Input Type | Required |
|-------|-----------|----------|
| Timestamp | `datetime-local` + TZ select | Yes |
| End Time | `datetime-local` (same TZ) | No |
| Description | `textarea` | No |
| Actor | `text` (datalist with existing actors) | No |
| Tags | `text` (comma-separated input; existing tags are offered through a datalist) | No |
| Images | `file` (multiple, accept=image/*) + drag & drop | No |

**Input TZ select:** the incident TZ is the default; UTC and the major IANA
zones can be chosen as well. Every option shows its UTC offset. On save the
offset is computed in the selected TZ and normalized to UTC.

---

## 6. CLI Interface

### Server (default)

```
ir-timeline [flags]

Flags:
  --db <path>         SQLite database path (default: timeline.db)
  --port <number>     HTTP server port (default: 8888)
  --no-browser        Don't auto-open browser
  --tz <timezone>     IANA timezone (e.g. Asia/Tokyo); defaults to system local
  --version           Show version
```

Starting it brings up the HTTP server and opens the default browser
automatically. If the DB file does not exist it is created (with automatic table
migration). SIGINT/SIGTERM shut it down gracefully (5 second timeout).

### Import subcommand

```
ir-timeline import [flags] <file>

Flags:
  --db <path>         SQLite database path (default: timeline.db)
  --format <fmt>      Input format: json or csv (auto-detected from extension)
```

**JSON format:**
```json
[
  {"timestamp":"2026-04-01T14:00:00+09:00","description":"...","actor":"...","tags":["..."]}
]
```

**CSV format (header required, column order free):**
```csv
timestamp,timestamp_end,description,actor,tags,input_tz
2026-04-01T14:00:00+09:00,,Event description,SOC,"detection,analysis",Asia/Tokyo
```

### Timezone resolution priority

1. The `--tz` CLI flag
2. The DB meta `timezone` key
3. The `$TZ` environment variable
4. The IANA name taken from the `/etc/localtime` symbolic link
5. Fallback: `UTC`

---

## 7. Security Considerations

| Area | Approach |
|------|----------|
| **Binding** | localhost (127.0.0.1) only |
| **SQL Injection** | Parameterized queries (`?` placeholders) only |
| **XSS** | The DOM API (`textContent`, `createElement`) only. `innerHTML` is not used |
| **File Upload** | 10MB limit, `image/*` MIME type only, header + magic byte validation |
| **CSRF** | localhost-only plus a SameSite cookie is enough. The premise is that it is never exposed externally |
| **Graceful Shutdown** | SIGINT/SIGTERM stop the HTTP server normally and close the DB |

---

## 8. Technology Stack

| Component | Choice | Reason |
|-----------|--------|--------|
| Language | Go | Single binary, cross-compile, no CGO required |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO required |
| HTTP router | `net/http` (Go 1.22+ routing) | No external dependency needed |
| Frontend | Vanilla HTML/CSS/JS | No build step, bundled through embed.FS |
| CSS | Custom (CSS variables) | Theme switching and i18n, and it stays light |
| Embed | `embed.FS` | Bundles the web assets into the binary |

---

## 9. Build & Distribution

```makefile
make build       # → dist/ir-timeline
make build-all   # → dist/ir-timeline-{os}-{arch} (5 platforms, CGO_ENABLED=0)
make test        # → go test ./... -v
make check       # → test + build
make clean       # → rm -rf dist/
```

**VERSION:** taken automatically from `git describe --tags --always --dirty`. Embedded with `-X main.version`.

**Supported platforms:** linux/amd64, linux/arm64, darwin/arm64, windows/amd64 (darwin is arm64-only)

**Release process:**
1. Update `CHANGELOG.md` → commit `chore: release vX.Y.Z`
2. `git tag vX.Y.Z && git push origin main --tags`
3. `gh release create` (no assets)
4. `make build-all`
5. Zip each binary + README.md
6. `gh release upload` the zips one by one
7. Update the umbrella (cybersecurity-series) submodule pointer
8. Update the org profile README
