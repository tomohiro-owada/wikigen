# wikigen

GitHub リポジトリのソースコードから自動で Wiki (Markdown) を生成する Go ワンバイナリ CLI。

Claude Code (`claude -p`) がリポジトリのコードを直接読み、GitHub Wiki 互換のドキュメントを生成します。

## アーキテクチャ

```
wikigen → git clone (SSH) → claude -p --add-dir ./repo
                                 │
                                 ├── Read (ソースコード読み取り)
                                 ├── Grep (パターン検索)
                                 ├── Glob (ファイル探索)
                                 ├── Bash (git log 等)
                                 └── Write (.md ファイル直接書き出し)
```

Docker、Ollama、embedding 一切不要。

## 前提条件

- Go 1.22+
- git（SSH認証設定済み、または PAT）
- `claude` CLI がインストール・認証済み

## セットアップ

```bash
git clone git@github.com:tomohiro-owada/wikigen.git
cd wikigen
cp .env.example .env
# 必要に応じて .env を編集
go build -o wikigen .
```

## 使い方

```bash
# 単一リポジトリ
./wikigen owner/repo

# 複数リポジトリ
./wikigen owner/repo1 owner/repo2

# ファイルから一括
./wikigen -f repos.txt

# リポジトリ並列 + ページ並列
./wikigen -f repos.txt -p 2 -pp 5

# モデル指定
./wikigen -f repos.txt -model haiku

# 英語で生成
./wikigen -f repos.txt -lang en
```

## repos.txt の書式

```
# 単独wiki（リポジトリごとに1つのwiki）
owner/repo1
owner/repo2

# マルチリポwiki（複数リポジトリを1つのwikiにまとめる）
myproject:owner/frontend-repo
myproject:owner/backend-repo
myproject:owner/shared-repo
```

## 出力形式

GitHub Wiki 互換のディレクトリ構成で出力されます。`{repo}.wiki.git` にそのまま push 可能。

```
wiki-output/{project}/
  Home.md           ← トップページ（目次）
  _Sidebar.md       ← ナビゲーション
  System-Architecture.md
  API-Specification.md
  Data-Model.md
  ...
  _errors.log       ← エラーがあった場合のみ作成
```

### GitHub Wiki への push

```bash
git clone git@github.com:owner/repo.wiki.git
cp -r wiki-output/repo/* repo.wiki/
cd repo.wiki
git add -A && git commit -m "Update wiki" && git push
```

## オプション

全てのオプションは `.env` でも設定可能です。コマンドラインフラグが優先されます。

| フラグ | 環境変数 | デフォルト | 説明 |
|---|---|---|---|
| `-f` | - | - | リポジトリリストファイル |
| `-r` | - | - | カンマ区切りリポジトリ |
| `-token` | `GITHUB_TOKEN` | (空=SSH) | GitHub PAT。未設定時はSSHでclone |
| `-model` | `CLAUDE_MODEL` | - | Claude モデル (haiku, sonnet, opus) |
| `-o` | `WIKI_OUTPUT_DIR` | `./wiki-output` | 出力ディレクトリ |
| `-clone-dir` | `WIKI_CLONE_DIR` | `./.repos` | clone先ディレクトリ |
| `-p` | `WIKI_PARALLEL` | `1` | リポジトリ並列数 |
| `-pp` | `WIKI_PAGE_PARALLEL` | `3` | ページ並列数（リポジトリごと） |
| `-lang` | `WIKI_LANGUAGE` | `ja` | 出力言語 |
| `-log` | - | stderr | ログファイルパス |

## 認証

| 方式 | 設定 | 用途 |
|---|---|---|
| SSH | `git` のSSH鍵設定済み | デフォルト。PAT不要 |
| PAT | `.env` に `GITHUB_TOKEN` を設定 | SSH未設定の環境向け |

## ドキュメント生成方針

コードベースから以下のカテゴリのドキュメントを自動生成します。
ページ数はリポジトリの規模に応じて動的に決定されます。

### A. コードから確実に生成（事実ベース）
- システム概要、アーキテクチャ、API仕様、データモデル
- ルーティング、状態管理、コンポーネント一覧
- 設定・環境変数、ビルド・デプロイ、テスト構成
- 認証・認可、エラーハンドリング、外部連携

### B. コードパターンからの推論（高い確度）
- 処理フロー、セキュリティ設計、パフォーマンス考慮

コードに根拠がないものは生成しません。
