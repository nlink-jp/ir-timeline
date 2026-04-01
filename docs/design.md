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
| **Modern UI** | ブラウザベース SPA、ダーク/ライトテーマ、レスポンシブ |
| **Portable** | DB ファイルをコピーするだけで別 PC に持ち出せる |

### Non-Goals

- マルチユーザー同時編集
- LLM による自動分析（→ ir-tracker が担当）
- リモートサーバーデプロイ

---

## 2. Architecture

```
┌─────────────────────────────────────────────────┐
│                   ir-timeline                    │
│                  (Go binary)                     │
│                                                  │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │ main.go  │→ │handler.go│→ │  storage.go   │  │
│  │ (flags,  │  │(HTTP API)│  │ (SQLite CRUD) │  │
│  │  server) │  └──────────┘  └───────┬───────┘  │
│  └──────────┘        ↑               │          │
│                      │               ▼          │
│              ┌───────────────┐  ┌──────────┐    │
│              │  embed.FS     │  │ .db file │    │
│              │ (web/*)       │  │ (SQLite) │    │
│              └───────────────┘  └──────────┘    │
└─────────────────────────────────────────────────┘
         ↕ HTTP (localhost only)
┌─────────────────────────────────────────────────┐
│              Web Browser (SPA)                   │
│  index.html + inline CSS/JS                      │
│  - Timeline view                                 │
│  - Event CRUD (modal forms)                      │
│  - Image upload / preview                        │
│  - Tag management                                │
│  - Markdown export                               │
└─────────────────────────────────────────────────┘
```

### File Structure

```
ir-timeline/
├── main.go            # Entry point, flags, HTTP server, auto-open browser
├── storage.go         # SQLite schema, migration, CRUD operations
├── handler.go         # HTTP handlers (REST API + static file serving)
├── storage_test.go    # Storage layer tests
├── handler_test.go    # HTTP handler tests
├── web/
│   └── index.html     # SPA (HTML + embedded CSS + JS)
├── docs/
│   └── design.md      # This document
├── Makefile           # build, test, clean, build-all
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

1 インシデント = 1 SQLite ファイル。テーブルは 3 つ。

### 3.1 `meta` — インシデントメタデータ

KV ストア形式。タイトルや作成日時を保持。

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT PK | キー名 |
| `value` | TEXT NOT NULL | 値 |

**Initial keys:**

| Key | Example Value | Description |
|-----|---------------|-------------|
| `title` | `"2026-04-01 フィッシング対応"` | インシデントタイトル |
| `case_id` | `"INC-2026-0042"` | ケース ID（チケット番号等、任意） |
| `created_at` | `"2026-04-01T14:00:00+09:00"` | 作成日時 |

### 3.2 `events` — タイムラインイベント

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK AUTOINCREMENT | イベント ID |
| `timestamp` | TEXT NOT NULL | イベント発生時刻 (ISO 8601) |
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

events [id PK, timestamp, description, actor, created_at, updated_at]
  │
  ├─ 1:N ─→ event_tags [event_id FK + tag PK]
  │
  └─ 1:N ─→ event_images [id PK, event_id FK, filename, content_type, data, created_at]
```

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
| GET | `/api/meta` | — | `{"title": "...", "case_id": "...", "created_at": "..."}` |
| PUT | `/api/meta` | `{"title": "...", "case_id": "..."}` | `{"ok": true}` |

### 4.3 Events API

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| GET | `/api/events` | — | `[{event}, ...]` sorted by timestamp |
| POST | `/api/events` | `{timestamp, description, actor, tags}` | `{event}` |
| PUT | `/api/events/:id` | `{timestamp, description, actor, tags}` | `{event}` |
| DELETE | `/api/events/:id` | — | `{"ok": true}` |

**Event JSON shape:**

```json
{
  "id": 1,
  "timestamp": "2026-04-01T14:00:00+09:00",
  "description": "ユーザーから不審メール報告",
  "actor": "SOC Team",
  "tags": ["detection", "communication"],
  "created_at": "2026-04-01T14:05:00+09:00",
  "updated_at": "2026-04-01T14:05:00+09:00",
  "images": [
    {"id": 1, "event_id": 1, "filename": "screenshot.png", "content_type": "image/png"}
  ]
}
```

### 4.4 Images API

| Method | Path | Request | Response |
|--------|------|---------|----------|
| POST | `/api/events/:id/images` | multipart/form-data (`file` field) | `{image}` |
| GET | `/api/images/:id` | — | image binary (Content-Type set) |
| DELETE | `/api/images/:id` | — | `{"ok": true}` |

**制約:** 画像は 10MB 以下、MIME type は `image/*` のみ許可。

### 4.5 Tags API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/tags` | `["detection", "analysis", "containment", ...]` |

使用中のタグ一覧を返す（`SELECT DISTINCT tag FROM events WHERE tag != ''`）。

### 4.6 Export API

| Method | Path | Response |
|--------|------|----------|
| GET | `/api/export/markdown` | `text/markdown` ファイルダウンロード |

---

## 5. UI Design

### 5.1 View Modes

ツールバーのトグルで 2 つの表示モードを切り替える。

#### 5.1.1 List View（縦タイムライン）— デフォルト

イベントを時系列で上から下に表示。詳細情報・画像を含むカード形式。

```
┌─────────────────────────────────────────────────────┐
│  [ir-timeline]  INC-2026-0042  Incident Title   [☀/🌙]│
│  ─────────────────────────────────────────────────── │
│  [+ Add Event]  [Filter ▼]  [Export ▼]  [List|Chart]│
│  ─────────────────────────────────────────────────── │
│                                                      │
│   14:00          ● ── Detection ──────────────────── │
│                  │  ユーザーから不審メール報告        │
│                  │  Actor: SOC Team                   │
│                  │  [screenshot.png]                  │
│   ┄ +15min ┄    │                                    │
│   14:15          ● ── Analysis ───────────────────── │
│                  │  メールヘッダ解析開始              │
│                  │  Actor: Analyst-A                  │
│   ┄ +45min ┄    │                                    │
│   15:00          ● ── Containment ────────────────── │
│                  │  対象アカウントを一時停止          │
│                  │  Actor: IT Admin                   │
│                                                      │
└─────────────────────────────────────────────────────┘
```

#### 5.1.2 Chart View（横軸タイムライン / スイムレーン）

横軸を時間、縦軸をタグ（グループ）で分割したスイムレーン表示。
インシデント全体の時間経過とフェーズの並行関係を俯瞰できる。

```
┌─────────────────────────────────────────────────────────────┐
│  [ir-timeline]  INC-2026-0042  Incident Title        [☀/🌙]│
│  ─────────────────────────────────────────────────────────── │
│  [+ Add Event]  [Filter ▼]  [Export ▼]  [List|Chart]       │
│  ─────────────────────────────────────────────────────────── │
│                                                              │
│  Time →    14:00    14:15    14:30    15:00    15:30         │
│            ──┼────────┼────────┼────────┼────────┼──        │
│             │        │        │        │        │           │
│  Detection  ●────────┤        │        │        │           │
│             │        │        │        │        │           │
│  Analysis   │        ●────────●───────┤        │           │
│             │        │        │        │        │           │
│  Containment│        │        │        ●────────┤           │
│             │        │        │        │        │           │
│  Comms      │   ●────┤        │   ●────┤        │           │
│                                                              │
│  ● = event marker  (hover/click for detail popup)           │
└─────────────────────────────────────────────────────────────┘
```

**Chart View の特徴:**

| Feature | Detail |
|---------|--------|
| **Swimlane** | タグごとに横レーンを生成。タグなしイベントは "(untagged)" レーンに配置 |
| **Time Axis** | 自動スケーリング（イベント範囲に合わせてズーム）。ドラッグで横スクロール |
| **Markers** | イベントをドット+縦線で描画。タグ色で着色 |
| **Hover Popup** | マーカーにホバーで概要（時刻、説明、Actor）をツールチップ表示 |
| **Click Detail** | クリックで編集モーダルを開く（List View と同じモーダルを共有） |
| **Zoom** | マウスホイールまたはピンチで時間軸を拡大/縮小 |
| **Lane Reorder** | レーンをドラッグで並び替え可能（表示順のみ、DB に影響なし） |

### 5.2 Common Features（両ビュー共通）

| Feature | Detail |
|---------|--------|
| **View Toggle** | ツールバーの [List / Chart] ボタンで切替。選択は localStorage に保存 |
| **Time Delta** | List View: イベント間に経過時間表示。Chart View: 軸の目盛りで把握 |
| **Tag Color** | タグ名に基づいてカラーを自動割当。タグごとにフィルタ可能 |
| **Image Attach** | イベントに画像をドラッグ＆ドロップまたはファイル選択で添付 |
| **Image Preview** | サムネイル表示、クリックで拡大モーダル |
| **Inline Edit** | イベントカードまたはマーカーをクリックで編集モーダル表示 |
| **Dark/Light** | テーマ切替。localStorage に保存 |
| **Markdown Export** | ボタン一つでタイムラインを Markdown ファイルとしてダウンロード |
| **Responsive** | モバイルでは List View のみ（Chart View は横幅が必要なため非表示） |

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
| Timestamp | `datetime-local` | Yes |
| Description | `textarea` | No |
| Actor | `text` (datalist with existing actors) | No |
| Tags | `text` (カンマ区切り入力、datalist で既存タグを候補表示) | No |
| Images | `file` (multiple, accept=image/*) | No |

---

## 6. CLI Interface

```
ir-timeline [flags]

Flags:
  --db <path>         SQLite database path (default: timeline.db)
  --port <number>     HTTP server port (default: 8888)
  --no-browser        Don't auto-open browser
  --version           Show version
```

起動すると HTTP サーバーが立ち上がり、デフォルトブラウザが自動で開く。
DB ファイルが存在しない場合は新規作成（テーブル自動マイグレーション）。

---

## 7. Security Considerations

| Area | Approach |
|------|----------|
| **Binding** | localhost (127.0.0.1) only。0.0.0.0 バインド不可 |
| **SQL Injection** | Parameterized queries (`?` placeholders) のみ使用 |
| **XSS** | DOM API (`textContent`, `createElement`) のみ。`innerHTML` 不使用 |
| **File Upload** | 10MB 上限、`image/*` MIME type のみ、Content-Type 検証 |
| **CSRF** | localhost-only + SameSite cookie で十分。外部公開しない前提 |

---

## 8. Technology Stack

| Component | Choice | Reason |
|-----------|--------|--------|
| Language | Go | Single binary, cross-compile |
| SQLite driver | `modernc.org/sqlite` | Pure Go, no CGO required |
| HTTP router | `net/http` (std) | No external dependency needed |
| Frontend | Vanilla HTML/CSS/JS | No build step, embed.FS で同梱 |
| CSS | Custom (CSS variables) | テーマ切替、軽量 |
| Embed | `embed.FS` | Web assets をバイナリに同梱 |

---

## 9. Build & Distribution

```makefile
# Single platform
make build          # → dist/ir-timeline

# All platforms (for release)
make build-all      # → dist/ir-timeline-{os}-{arch}

# Test
make test           # → go test ./...
```

**Supported platforms:** linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
