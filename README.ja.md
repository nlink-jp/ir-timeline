# ir-timeline

インシデントレスポンスのタイムライン記録ツール — シングルバイナリ、ブラウザ UI で、テキスト・画像・タグ・時間差分を管理。

[English README](README.md)

## コンセプト

Excel でのタイムライン管理を、モダンなローカルツールに置き換え:

- **シングルバイナリ** — ランタイム依存なし（Python/Node.js 不要）
- **DB ファイル 1 つ** — 1 インシデント = 1 SQLite ファイル。画像も BLOB で格納
- **ブラウザ UI** — モダンなタイムライン表示、ダーク/ライトテーマ
- **ポータブル** — DB ファイルをコピーするだけで共有・アーカイブ

## クイックスタート

```bash
# ビルド
make build

# 実行（timeline.db を自動作成、ブラウザが開く）
./dist/ir-timeline

# DB ファイルを指定
./dist/ir-timeline --db incident-2026-04-01.db

# ポート指定、ブラウザ自動起動なし
./dist/ir-timeline --port 9090 --no-browser
```

## 機能

### List View（縦タイムライン）

イベントを時系列で上から下にカード表示。説明・Actor・タグ・画像・時間差分を含む。

### Chart View（横軸スイムレーン）

横軸に時間、縦軸にタグをスイムレーンとして配置。複数タグを持つイベントは全該当レーンに表示。ホバーで詳細、クリックで編集。

### 共通機能

| 機能 | 詳細 |
|------|------|
| **複数タグ** | 各イベントに複数タグを付与し、柔軟にグループ化 |
| **タグ色分け** | IR フェーズには固定色、カスタムタグにはハッシュベースの自動色 |
| **画像添付** | ドラッグ＆ドロップまたはファイル選択、SQLite に BLOB 保存 |
| **画像プレビュー** | サムネイル表示、クリックで拡大 |
| **時間差分** | イベント間の経過時間を自動計算・表示 |
| **ダーク/ライト** | テーマ切替、設定は localStorage に保存 |
| **ケース ID** | ヘッダーにバッジ表示、編集可能 |
| **タグフィルタ** | 特定タグでタイムラインを絞り込み |
| **Markdown エクスポート** | タイムラインを `.md` ファイルとしてダウンロード |

## CLI フラグ

```
ir-timeline [flags]

  --db <path>       SQLite データベースパス（デフォルト: timeline.db）
  --port <number>   HTTP サーバーポート（デフォルト: 8888）
  --no-browser      ブラウザ自動起動を無効化
  --version         バージョン表示
```

## 定義済みタグカラー

| タグ | 色 |
|------|-----|
| `detection` | 青 |
| `analysis` | 紫 |
| `containment` | オレンジ |
| `eradication` | 赤 |
| `recovery` | 緑 |
| `communication` | ティール |
| `lesson` | インディゴ |

その他のタグ名にはハッシュベースの自動カラーが割り当てられます。

## セキュリティ

- `127.0.0.1` のみにバインド — リモートアクセス不可
- 全 SQL クエリはパラメータ化ステートメントを使用
- DOM API (`textContent` / `createElement`) のみ使用、`innerHTML` 不使用
- 画像アップロードは 10 MB 上限、`image/*` MIME タイプのみ

## アーキテクチャ

```
ir-timeline (Go バイナリ)
├── main.go         — エントリーポイント、フラグ、HTTP サーバー
├── storage.go      — SQLite スキーマ、CRUD 操作
├── handler.go      — REST API ハンドラ
└── web/
    └── index.html  — SPA (HTML + CSS + JS、embed.FS でバイナリに同梱)
```

詳細設計は [docs/design.md](docs/design.md) を参照。

## ビルド

```bash
make build          # → dist/ir-timeline
make test           # → go test ./...
make build-all      # → 5 プラットフォーム対応バイナリ
make clean          # → dist/ を削除
```

## cybersecurity-series の一部

ir-timeline は [cybersecurity-series](https://github.com/nlink-jp/cybersecurity-series) の一部です —
脅威インテリジェンス、インシデントレスポンス、セキュリティ運用のための AI 活用ツール群。
