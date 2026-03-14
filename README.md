# wikigen

A single Go binary CLI that generates GitHub Wiki from source code using Claude Code.

Claude Code (`claude -p`) reads the repository code directly — using Read, Grep, Glob, and Bash tools — and writes GitHub Wiki-compatible Markdown files.

---

**[English](#english)** | **[日本語](#日本語)**

---

## English

### Architecture

```
wikigen → git clone (SSH) → claude -p --add-dir ./repo
                                 │
                                 ├── Read (source code)
                                 ├── Grep (pattern search)
                                 ├── Glob (file discovery)
                                 ├── Bash (git log, etc.)
                                 └── Write (.md files directly)
```

No Docker, Ollama, or embedding required. Single binary, zero infrastructure.

### Prerequisites

- Go 1.22+
- git (SSH key configured, or PAT)
- `claude` CLI installed and authenticated (`claude -p "hello"` should work)

### Setup

```bash
git clone https://github.com/tomohiro-owada/wikigen.git
cd wikigen
cp .env.example .env
# Edit .env as needed
go build -o wikigen .
```

### Quick Start

```bash
# Preview structure without generating (dry run)
./wikigen -dry-run owner/repo

# Generate wiki
./wikigen owner/repo

# Batch from file with parallelism
./wikigen -f repos.txt -p 2 -pp 5

# Retry only failed pages
./wikigen -retry

# Get JSON results
./wikigen -json owner/repo
```

### Usage

```bash
# Single repository
./wikigen owner/repo

# Multiple repositories
./wikigen owner/repo1 owner/repo2

# Batch from file
./wikigen -f repos.txt

# Repo-level + page-level parallelism
./wikigen -f repos.txt -p 2 -pp 5

# Specify model
./wikigen -f repos.txt -model haiku

# Generate in English
./wikigen -f repos.txt -lang en

# Dry run (structure only, no page generation)
./wikigen -dry-run -f repos.txt

# JSON output (structured results to stdout)
./wikigen -json -f repos.txt

# Retry failed pages only
./wikigen -retry
```

### repos.txt Format

```
# Standalone wiki (one wiki per repo)
owner/repo1
owner/repo2

# Multi-repo wiki (multiple repos merged into one wiki)
myproject:owner/frontend-repo
myproject:owner/backend-repo
myproject:owner/shared-repo
```

Multi-repo wikis generate cross-repository documentation — architecture pages that span all repos, showing how services interact.

### Output Format

Outputs GitHub Wiki-compatible directory structure. Push directly to `{repo}.wiki.git`.

```
wiki-output/{project}/
  Home.md              # Landing page with table of contents
  _Sidebar.md          # Navigation sidebar
  System-Architecture.md
  API-Specification.md
  Data-Model.md
  ...
  _errors.log          # Created only if errors occurred
```

#### Push to GitHub Wiki

```bash
git clone git@github.com:owner/repo.wiki.git
cp -r wiki-output/repo/* repo.wiki/
cd repo.wiki
git add -A && git commit -m "Update wiki" && git push
```

### Options

All options can be set via `.env` file. CLI flags take precedence over env vars.

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `-f` | - | - | Repository list file |
| `-r` | - | - | Comma-separated repos |
| `-token` | `GITHUB_TOKEN` | (empty=SSH) | GitHub PAT. If empty, SSH is used |
| `-model` | `CLAUDE_MODEL` | - | Claude model (haiku, sonnet, opus) |
| `-o` | `WIKI_OUTPUT_DIR` | `./wiki-output` | Output directory |
| `-clone-dir` | `WIKI_CLONE_DIR` | `./.repos` | Clone directory |
| `-p` | `WIKI_PARALLEL` | `1` | Parallel repos |
| `-pp` | `WIKI_PAGE_PARALLEL` | `3` | Parallel pages per repo |
| `-lang` | `WIKI_LANGUAGE` | `ja` | Output language |
| `-log` | - | stderr | Log file path |
| `-retry` | - | false | Retry failed pages only |
| `-dry-run` | - | false | Determine structure only |
| `-json` | - | false | Output results as JSON to stdout |

### Authentication

| Method | Config | Use Case |
|---|---|---|
| SSH | SSH key registered with GitHub | Default. No PAT needed |
| PAT | Set `GITHUB_TOKEN` in `.env` | For environments without SSH |

### Input Validation

wikigen validates all repository inputs:
- Must match `owner/repo` format
- Rejects path traversal (`..`)
- Rejects shell injection characters (`;`, `&`, `|`, etc.)

### Error Handling & Retry

1. **Auto-retry**: Each page is retried up to 3 times automatically
2. **`-retry` flag**: Scans `wiki-output/` for failed pages and regenerates only those
3. **`_errors.log`**: Timestamped error details per project
4. **Incremental save**: Each page is saved immediately after generation — partial results are preserved if the process is interrupted

### Progress Display

Real-time progress with percentage:

```
── Progress: 1/3 wikis (33%) ──
  dala-delivery  📝 5/20 (25%) API-Specification
  gmn            📝 12/15 (80%) Tool-System
```

### JSON Output

Use `-json` for structured output (useful for scripting):

```bash
./wikigen -json owner/repo 2>/dev/null | jq '.[] | .project, .total_pages, .status'
```

### Documentation Policy

Generates documentation from the following categories based on actual source code.
Page count is dynamically determined based on repository complexity.

#### A. Factual (directly from code)
- System overview, architecture, API specs, data models
- Routing, state management, component catalog
- Config, build/deploy, testing, auth, error handling, integrations

#### B. High-confidence inference (from code patterns)
- Processing flows, security design, performance considerations

Nothing is generated without code evidence. No speculation.

### GitHub Actions — Auto-update Wiki on Push

Automatically regenerate your GitHub Wiki whenever source code changes.

#### Setup

1. **Enable Wiki** in your repository settings (Settings → Features → Wiki)
2. **Create an initial Wiki page** via the Wiki tab (this initializes `.wiki.git`)
3. **Generate an OAuth token**:
   ```bash
   claude setup-token
   ```
4. **Add the token to Secrets**: Settings → Secrets → Actions → `CLAUDE_CODE_OAUTH_TOKEN`
5. **Add the workflow** `.github/workflows/wiki.yml`:

```yaml
name: Generate Wiki

on:
  workflow_dispatch:
  push:
    branches: [main]
    paths:
      - '*.go'        # Adjust to your source files
      - 'go.mod'

permissions:
  contents: write

jobs:
  wiki:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Build wikigen
        run: go build -o wikigen .

      - uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install Claude CLI
        run: npm install -g @anthropic-ai/claude-code

      - name: Generate wiki
        env:
          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
        run: ./wikigen -lang en -pp 3 -model haiku -token "${{ secrets.GITHUB_TOKEN }}" owner/repo

      - name: Push to wiki
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          git clone https://x-access-token:${GITHUB_TOKEN}@github.com/owner/repo.wiki.git wiki-repo
          cp -r wiki-output/repo/* wiki-repo/
          cd wiki-repo
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git diff --staged --quiet || (git commit -m "Update wiki" && git push)
```

Replace `owner/repo` with your repository. The wiki auto-updates on every push to `main` that changes source files, or can be triggered manually from the Actions tab.

> **Note**: The OAuth token expires after approximately 1 year. Use `claude setup-token` to regenerate when needed. Alternatively, use `ANTHROPIC_API_KEY` for stable long-term usage (API billing applies).

### Acknowledgements

This project was inspired by [DeepWiki-Open](https://github.com/AsyncFuncAI/deepwiki-open), an AI-powered wiki generator that uses RAG and embedding for documentation generation. wikigen takes a different approach — replacing the RAG/embedding pipeline with Claude Code's native tool use (Read, Grep, Glob, Bash) for direct source code access, eliminating the need for Docker, Ollama, or any embedding infrastructure.

### Claude Code Integration

wikigen includes a `.claude/skills/wikigen/SKILL.md` skill file. When working in the wikigen directory, Claude Code can reference usage patterns and best practices automatically.

---

## 日本語

### アーキテクチャ

```
wikigen → git clone (SSH) → claude -p --add-dir ./repo
                                 │
                                 ├── Read (ソースコード読み取り)
                                 ├── Grep (パターン検索)
                                 ├── Glob (ファイル探索)
                                 ├── Bash (git log 等)
                                 └── Write (.md ファイル直接書き出し)
```

Docker、Ollama、embedding 一切不要。ワンバイナリ、インフラ不要。

### 前提条件

- Go 1.22+
- git（SSH認証設定済み、または PAT）
- `claude` CLI がインストール・認証済み（`claude -p "hello"` が動くこと）

### セットアップ

```bash
git clone https://github.com/tomohiro-owada/wikigen.git
cd wikigen
cp .env.example .env
# 必要に応じて .env を編集
go build -o wikigen .
```

### クイックスタート

```bash
# 構造のプレビュー（dry run）
./wikigen -dry-run owner/repo

# wiki 生成
./wikigen owner/repo

# 一括生成（並列）
./wikigen -f repos.txt -p 2 -pp 5

# 失敗ページのリトライ
./wikigen -retry

# JSON で結果取得
./wikigen -json owner/repo
```

### 使い方

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

# Dry run（構造決定のみ）
./wikigen -dry-run -f repos.txt

# JSON 出力
./wikigen -json -f repos.txt

# 失敗ページのリトライ
./wikigen -retry
```

### repos.txt の書式

```
# 単独wiki（リポジトリごとに1つのwiki）
owner/repo1
owner/repo2

# マルチリポwiki（複数リポジトリを1つのwikiにまとめる）
myproject:owner/frontend-repo
myproject:owner/backend-repo
myproject:owner/shared-repo
```

マルチリポwikiではリポジトリ間の連携を含む横断的なドキュメントが生成されます。

### 出力形式

GitHub Wiki 互換のディレクトリ構成で出力。`{repo}.wiki.git` にそのまま push 可能。

```
wiki-output/{project}/
  Home.md              ← トップページ（目次）
  _Sidebar.md          ← ナビゲーション
  System-Architecture.md
  API-Specification.md
  Data-Model.md
  ...
  _errors.log          ← エラーがあった場合のみ作成
```

#### GitHub Wiki への push

```bash
git clone git@github.com:owner/repo.wiki.git
cp -r wiki-output/repo/* repo.wiki/
cd repo.wiki
git add -A && git commit -m "Update wiki" && git push
```

### オプション

全てのオプションは `.env` でも設定可能。コマンドラインフラグが優先。

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
| `-retry` | - | false | 失敗ページのみ再生成 |
| `-dry-run` | - | false | 構造決定のみ |
| `-json` | - | false | 結果をJSONでstdoutに出力 |

### 認証

| 方式 | 設定 | 用途 |
|---|---|---|
| SSH | `git` のSSH鍵設定済み | デフォルト。PAT不要 |
| PAT | `.env` に `GITHUB_TOKEN` を設定 | SSH未設定の環境向け |

### 入力バリデーション

全てのリポジトリ入力を検証：
- `owner/repo` 形式のみ受け付け
- パストラバーサル（`..`）を拒否
- シェルインジェクション文字（`;`, `&`, `|` 等）を拒否

### エラー処理とリトライ

1. **自動リトライ**: 各ページは最大3回自動リトライ
2. **`-retry` フラグ**: `wiki-output/` 内の失敗ページのみを再生成
3. **`_errors.log`**: プロジェクトごとにタイムスタンプ付きエラー詳細を記録
4. **即時保存**: 各ページは生成完了時点で保存 — プロセスが中断されても生成済みページは保持

### 進捗表示

パーセント付きリアルタイム進捗：

```
── Progress: 1/3 wikis (33%) ──
  dala-delivery  📝 5/20 (25%) API-Specification
  gmn            📝 12/15 (80%) ツールシステム
```

### JSON 出力

`-json` でスクリプト連携に便利な構造化出力：

```bash
./wikigen -json owner/repo 2>/dev/null | jq '.[] | .project, .total_pages, .status'
```

### ドキュメント生成方針

コードベースから以下のカテゴリのドキュメントを自動生成。
ページ数はリポジトリの規模に応じて動的に決定。

#### A. コードから確実に生成（事実ベース）
- システム概要、アーキテクチャ、API仕様、データモデル
- ルーティング、状態管理、コンポーネント一覧
- 設定・環境変数、ビルド・デプロイ、テスト構成
- 認証・認可、エラーハンドリング、外部連携

#### B. コードパターンからの推論（高い確度）
- 処理フロー、セキュリティ設計、パフォーマンス考慮

コードに根拠がないものは生成しません。推測は一切行いません。

### GitHub Actions — Push 時に Wiki を自動更新

ソースコード変更時に GitHub Wiki を自動再生成できます。

#### セットアップ

1. **Wiki を有効化** — リポジトリの Settings → Features → Wiki
2. **初期ページを作成** — Wiki タブから手動で1ページ作成（`.wiki.git` の初期化に必要）
3. **OAuth トークンを生成**:
   ```bash
   claude setup-token
   ```
4. **Secrets にトークンを追加** — Settings → Secrets → Actions → `CLAUDE_CODE_OAUTH_TOKEN`
5. **ワークフロー `.github/workflows/wiki.yml` を追加**（英語セクションの YAML 例を参照）

Push 時に自動実行、または Actions タブから手動実行できます。

> **注意**: OAuth トークンの有効期限は約1年です。`claude setup-token` で再生成してください。安定運用には `ANTHROPIC_API_KEY`（API課金）も使用できます。

### 謝辞

本プロジェクトは [DeepWiki-Open](https://github.com/AsyncFuncAI/deepwiki-open)（RAG と embedding を用いた AI Wiki ジェネレーター）にインスパイアされて開発しました。wikigen は RAG/embedding パイプラインを Claude Code のネイティブツール（Read, Grep, Glob, Bash）による直接的なソースコードアクセスに置き換えることで、Docker・Ollama・embedding インフラを不要にしています。

### Claude Code 連携

`.claude/skills/wikigen/SKILL.md` スキルファイルを同梱。wikigen ディレクトリで作業中の Claude Code が使い方とベストプラクティスを自動参照できます。
