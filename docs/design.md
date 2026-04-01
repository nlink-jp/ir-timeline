# ir-timeline Design Document

## 1. Overview

IR（インシデントレスポンス）対応時のタイムライン記録・管理ツール。
Excel によるタイムライン管理を置き換え、ブラウザベースのモダン UI で
イベントの記録・画像添付・タグ分類・時間差分表示を提供する。

### Design Goals

| Goal | Detail |
|------|--------|
| **Single binary** | Go でビルド、CGO 不要（`modernc.org/sqlite`） |
| **Single DB file** | 1 インシデント = 1 SQLite ファイル。画像も BLOB で DB 内に格納 |
| **Zero config** | バイナリ実行のみで起動。外部サービス依存なし |
| **Modern UI** | ブラウザベース SPA、ダーク/ライトテーマ、日英 i18n |
| **Portable** | DB ファイルをコピーするだけで別 PC に持ち出せる |

### Non-Goals

- マルチユーザー同時編集
- LLM による自動分析（→ ir-tracker が担当）
- リモートサーバーデプロイ

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
│   └── design.md      # This document
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

1 インシデント = 1 SQLite ファイル。テーブルは 4 つ。

### Timestamp 保存ルール

- **DB 内の全 timestamp は UTC (ISO 8601, `Z` suffix) で保存する**
- 入力時の TZ は `events.input_tz` カラムに記録（元の時間帯に復元可能）
- 保存時に `toUTC()` で正規化、表示時にインシデント TZ に変換
- SQLite の `ORDER BY timestamp ASC` がそのまま正しい時系列順になる

### 3.1 `meta` — インシデントメタデータ

KV ストア形式。タイトル、ケース ID、タイムゾーン等を保持。

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT PK | キー名 |
| `value` | TEXT NOT NULL | 値 |

**Keys:**

| Key | Example Value | Description |
|-----|---------------|-------------|
| `title` | `"2026-04-01 フィッシング対応"` | インシデントタイトル |
| `case_id` | `"INC-2026-0042"` | ケース ID（チケット番号等、任意） |
| `timezone` | `"Asia/Tokyo"` | 表示用タイムゾーン (IANA) |
| `created_at` | `"2026-04-01T05:00:00Z"` | 作成日時 (UTC) |

### 3.2 `events` — タイムラインイベント

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK AUTOINCREMENT | イベント ID |
| `timestamp` | TEXT NOT NULL | イベント発生時刻 (UTC, ISO 8601) |
| `timestamp_end` | TEXT | 終了時刻 (UTC, 任意 — 期間イベント用) |
| `input_tz` | TEXT NOT NULL DEFAULT 'UTC' | 入力時のタイムゾーン (IANA) |
| `description` | TEXT NOT NULL DEFAULT '' | イベント内容（自由記述） |
| `actor` | TEXT NOT NULL DEFAULT '' | 対応者・チーム名 |
| `created_at` | TEXT NOT NULL | レコード作成時刻 |
| `updated_at` | TEXT NOT NULL | レコード更新時刻 |

**Indexes:** `idx_events_timestamp ON events(timestamp)`

### 3.3 `event_tags` — イベントタグ（多対多）

1 イベントに複数のタグを付与可能。タグがスイムレーンのグループになる。

| Column | Type | Description |
|--------|------|-------------|
| `event_id` | INTEGER NOT NULL FK→events(id) ON DELETE CASCADE | イベント ID |
| `tag` | TEXT NOT NULL | タグ名（例: detection, containment） |

**PK:** `(event_id, tag)` — 同一イベントに同じタグは重複不可

**Indexes:** `idx_event_tags_tag ON event_tags(tag)`

**備考:** Chart View のスイムレーンでは、複数タグを持つイベントは
該当する全レーンにマーカーが表示される。

### 3.4 `event_images` — イベント添付画像

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK AUTOINCREMENT | 画像 ID |
| `event_id` | INTEGER NOT NULL FK→events(id) ON DELETE CASCADE | 所属イベント |
| `filename` | TEXT NOT NULL | 元ファイル名 |
| `content_type` | TEXT NOT NULL | MIME type (image/png, image/jpeg 等) |
| `data` | BLOB NOT NULL | 画像バイナリ |
| `created_at` | TEXT NOT NULL | アップロード時刻 |

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

新カラム追加は `addColumnIfNotExists` パターンで既存 DB を自動マイグレーション:
- `timestamp_end TEXT` (v0.1.0)
- `input_tz TEXT NOT NULL DEFAULT 'UTC'` (v0.1.0)

---

## 4. API Design

Base URL: `http://127.0.0.1:{port}`

### 4.1 Static Files

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | SPA (index.html) を返す |

### 4.2 Meta API

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| GET | `/api/meta` | — | `{"title": "...", "case_id": "...", "timezone": "...", ...}` |
| PUT | `/api/meta` | `{"title": "...", "case_id": "...", "timezone": "..."}` | `{"ok": true}` |

### 4.3 Timezone API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/timezone` | `{"timezone": "Asia/Tokyo"}` |

CLIフラグ `--tz` > DB meta > システムローカルの優先順位で解決。

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

**制約:** 画像は 10MB 以下、MIME type は `image/*` のみ許可。Content-Type はヘッダーとマジックバイトの両方で検証。

### 4.6 Tags API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/tags` | `["detection", "analysis", "containment", ...]` |

使用中のタグ一覧を返す（`SELECT DISTINCT tag FROM event_tags`）。

### 4.7 Export API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/export/markdown` | `text/markdown` ファイルダウンロード（タイムゾーン変換済み） |

---

## 5. UI Design

### 5.1 View Modes

ツールバーのトグルで 2 つの表示モードを切り替える。

#### 5.1.1 List View（縦タイムライン）— デフォルト

イベントを時系列で上から下に表示。詳細情報・画像を含むカード形式。
期間イベントは「14:00 — 15:30 (1h30m)」の形式で開始〜終了+所要時間を表示。

#### 5.1.2 Chart View（横軸タイムライン / スイムレーン）

横軸を時間、縦軸をタグ（グループ）で分割したスイムレーン表示。
インシデント全体の時間経過とフェーズの並行関係を俯瞰できる。

**Chart View の特徴:**

| Feature | Detail |
|---------|--------|
| **Swimlane** | タグごとに横レーンを生成。タグなしイベントは "(untagged)" レーンに配置 |
| **Point/Range** | ポイントイベント→ドット、期間イベント→角丸バーで描画 |
| **Time Axis** | 自動スケーリング。前後5%パディング |
| **Cursor Bar** | マウス位置に縦線+日時ラベルをフローティング表示 |
| **Zoom** | マウスホイールでカーソル位置を中心にズーム（最大50x）。+/−ボタンも |
| **Pan** | ドラッグで時間軸を左右にパン |
| **Hover Popup** | マーカーにホバーで概要（時刻、説明、Actor）をツールチップ表示 |
| **Click Detail** | クリックで編集モーダルを開く（List View と同じモーダルを共有） |
| **Clip Path** | マーカーは描画領域外にはみ出さないようクリップ |

### 5.2 Common Features（両ビュー共通）

| Feature | Detail |
|---------|--------|
| **Sticky Header** | ヘッダー+ツールバーが `position: sticky` でスクロール時も固定 |
| **View Toggle** | [List / Chart] ボタンで切替。選択は localStorage に保存 |
| **Time Delta** | List View: イベント間に経過時間表示。Chart View: 軸の目盛り+カーソルバー |
| **Multi-Tag Filter** | チェックボックスドロップダウンで複数タグを選択（OR フィルタ）。タグ色ドット付き |
| **Image Attach** | イベントに画像をドラッグ＆ドロップまたはファイル選択で添付 |
| **Image Preview** | サムネイル表示、クリックで拡大ライトボックス |
| **Inline Edit** | イベントカードまたはマーカーをクリックで編集モーダル表示 |
| **Dark/Light** | テーマ切替。localStorage に保存 |
| **i18n** | 日本語/英語切替。localStorage に保存。全 UI テキスト対応 |
| **Markdown Export** | ボタン一つでタイムラインを Markdown ファイルとしてダウンロード（TZ変換済み） |

### 5.3 Tag Color Mapping

タグ名のハッシュ値から HSL カラーを自動生成。よく使う IR タグには固定色を割当：

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

イベント追加・編集用のモーダルダイアログ：

| Field | Input Type | Required |
|-------|-----------|----------|
| Timestamp | `datetime-local` + TZ select | Yes |
| End Time | `datetime-local` (same TZ) | No |
| Description | `textarea` | No |
| Actor | `text` (datalist with existing actors) | No |
| Tags | `text` (カンマ区切り入力、datalist で既存タグを候補表示) | No |
| Images | `file` (multiple, accept=image/*) + drag & drop | No |

**入力 TZ セレクト:** インシデント TZ がデフォルト。UTC や主要 IANA ゾーンも選択可。
各選択肢に UTC オフセットを表示。保存時に選択 TZ でオフセット計算→UTC に正規化。

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

起動すると HTTP サーバーが立ち上がり、デフォルトブラウザが自動で開く。
DB ファイルが存在しない場合は新規作成（テーブル自動マイグレーション）。
SIGINT/SIGTERM でグレースフルシャットダウン（5秒タイムアウト）。

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

1. `--tz` CLI フラグ
2. DB meta `timezone` キー
3. `$TZ` 環境変数
4. `/etc/localtime` シンボリックリンクから IANA 名を取得
5. フォールバック: `UTC`

---

## 7. Security Considerations

| Area | Approach |
|------|----------|
| **Binding** | localhost (127.0.0.1) only |
| **SQL Injection** | Parameterized queries (`?` placeholders) のみ使用 |
| **XSS** | DOM API (`textContent`, `createElement`) のみ。`innerHTML` 不使用 |
| **File Upload** | 10MB 上限、`image/*` MIME type のみ、ヘッダー+マジックバイト検証 |
| **CSRF** | localhost-only + SameSite cookie で十分。外部公開しない前提 |
| **Graceful Shutdown** | SIGINT/SIGTERM で HTTP サーバーを正常停止、DB をクローズ |

---

## 8. Technology Stack

| Component | Choice | Reason |
|-----------|--------|--------|
| Language | Go | Single binary, cross-compile, CGO 不要 |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO required |
| HTTP router | `net/http` (Go 1.22+ routing) | No external dependency needed |
| Frontend | Vanilla HTML/CSS/JS | No build step, embed.FS で同梱 |
| CSS | Custom (CSS variables) | テーマ切替・i18n 対応、軽量 |
| Embed | `embed.FS` | Web assets をバイナリに同梱 |

---

## 9. Build & Distribution

```makefile
make build       # → dist/ir-timeline
make build-all   # → dist/ir-timeline-{os}-{arch} (5 platforms, CGO_ENABLED=0)
make test        # → go test ./... -v
make check       # → test + build
make clean       # → rm -rf dist/
```

**VERSION:** `git describe --tags --always --dirty` で自動取得。`-X main.version` で埋め込み。

**Supported platforms:** linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64

**Release process:**
1. `CHANGELOG.md` 更新 → `chore: release vX.Y.Z` コミット
2. `git tag vX.Y.Z && git push origin main --tags`
3. `gh release create` (アセットなし)
4. `make build-all`
5. 各バイナリ + README.md を zip
6. zip を 1 つずつ `gh release upload`
7. アンブレラ (cybersecurity-series) サブモジュールポインタ更新
8. org profile README 更新
