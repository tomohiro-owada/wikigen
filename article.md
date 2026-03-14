---
title: "wikigen — コードベースから仕様ドキュメントを自動生成する"
emoji: "📖"
type: "tech"
topics: ["claudecode", "github", "wiki", "go", "documentation"]
published: false
---

## wikigen とは

[wikigen](https://github.com/tomohiro-owada/wikigen) は、GitHubリポジトリのソースコードから仕様ドキュメントを自動生成するCLIツール。

```bash
./wikigen owner/repo
```

これだけでリポジトリのコードを読み取り、GitHub Wiki互換のMarkdownドキュメント一式を生成する。Docker不要、embedding不要。`git` と `claude` CLIがあれば動く。

生成されるのは「AIがふわっと要約したもの」ではなく、コードに根拠のある仕様ドキュメント。API仕様、データモデル、アーキテクチャ設計、認証フローなど、JIS X 0160（ISO/IEC 12207）のドキュメント体系を参考に「コードから生成可能なもの」に絞って出力する。

## 背景

チームが増えてきてコードの全体像を把握するのが難しくなってきた。READMEはあるけど設計ドキュメントは古いまま。新しく入った人が「このAPIどこから呼ばれてるの？」「このテーブル何に使ってるの？」って毎回聞いてくる。

最新のコードから仕様がわかるドキュメントがあれば、少なくとも「まずドキュメント見て」と言える。GitHub Wikiに置けばアクセスも簡単。

## DeepWikiとの出会い

[DeepWiki](https://devin.ai/)（Devin社）がまさにこれをやってくれるサービスで、リポジトリを指定するとAIがコードを読んでWikiを生成してくれる。コードからドキュメントを生成するというアプローチは良かった。

ただ、自分のユースケース（プライベートリポジトリの一括生成）だと合わない部分があった。

- UIから毎回リポジトリURL・モデル・PATを入力する必要がある
- バッチ実行やCI/CDからの自動実行ができない
- プライベートリポジトリだとMCPの一部機能が未対応

OSSの [DeepWiki-Open](https://github.com/AsyncFuncAI/deepwiki-open) も試した。こちらは自前でホストできるのが良い。ただ、Docker + Ollama（embedding用）+ LLMバックエンドという構成で、依存が結構増える。

## 発想の転換

DeepWiki-Openの中身を読んでいたら、やってることは：

1. リポジトリをclone
2. ファイルをembeddingしてベクトルDB化（RAG）
3. 関連ファイルをLLMに渡してドキュメント生成

embeddingによるRAGは賢い仕組みだけど、依存が増えるのが気になった。Dockerコンテナ立てて、Ollamaでembeddingモデル動かして…と、ドキュメント生成のためにインフラを構築することになる。

ここでふと思った。Claude CodeのExplore機能ってそもそも優秀で、コードベースを渡せば自分で必要なファイルを探して読んでくれる。`claude -p` に `--add-dir` でリポジトリを渡せば、Read/Grep/Glob/Bashで直接コードを読める。RAGもembeddingも要らないんじゃないか。

```
DeepWiki方式:  clone → embedding → RAG検索 → LLM
wikigen方式:   clone → claude -p --add-dir ./repo → 直接読む
```

Claude Code自身がエージェントとしてコードベースを探索してくれるから、embeddingで取りこぼすリスクもない。

## wikigenを作った

Goのワンバイナリで、やることはシンプル：

1. `git clone` でリポジトリ取得
2. `claude -p --add-dir ./repo` でClaude Codeにコードを渡す
3. まずwiki構造（ページ一覧）を決めさせる
4. 各ページを並列で生成
5. GitHub Wiki互換のMarkdownファイルとして保存

```bash
./wikigen owner/repo
```

これだけ。Docker不要、Ollama不要、embedding不要。`git`と`claude` CLIがあれば動く。

### repos.txtで一括生成

```
# 単独wiki
tomohiro-owada/gmn

# マルチリポ（1つのwikiにまとめる）
mogecheck:mortgagefss/mogecheck-front-nuxt
mogecheck:mortgagefss/mogecheck-biz
mogecheck:mortgagefss/mogecheck-c
```

マルチリポ対応が地味に便利。フロントエンドとバックエンドのリポジトリが分かれてても、1つのwikiに「API呼び出しフロー」みたいな横断ドキュメントが生成される。

### 並列処理

```bash
./wikigen -f repos.txt -p 2 -pp 5
```

`-p 2` でリポジトリ2つ同時、`-pp 5` で各リポジトリ内のページ5つ同時生成。進捗もリアルタイムで見える。

```
[1/3 33%] dala-delivery 📝 5/20 (25%) API-Specification
```

### 失敗したページだけリトライ

LLMの生成は確率的なので、たまに失敗する。自動で3回リトライするけど、それでもダメな場合は：

```bash
./wikigen -retry
```

`wiki-output/` を走査して失敗ページだけ再生成。全部やり直さなくていい。

### GitHub Actionsで自動更新

```yaml
- name: Generate wiki
  env:
    CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
  run: ./wikigen -lang en -pp 3 -token "${{ secrets.GITHUB_TOKEN }}" owner/repo
```

mainにpushしたら自動でWikiが更新される。OAuth tokenはClaude Codeのサブスクリプション範囲で使えるので追加課金なし（トークン消費はある）。

## ドキュメント生成の方針

「コードから何を読み取ってドキュメントにするか」を真面目に考えた。JIS X 0160（ISO/IEC 12207ベース）のドキュメント体系を参考に、コードベースから生成可能なものを2段階に分類した。

**A. コードから確実に生成できるもの（事実ベース）**
- システム概要、アーキテクチャ、API仕様
- データモデル（マイグレーション、ORM定義から）
- 設定・環境変数、ビルド・デプロイ手順
- テスト構成、認証フロー

**B. コードパターンから高い確度で推論できるもの**
- 処理フロー（関数呼び出しチェーンから）
- セキュリティ設計（ミドルウェア、バリデーションから）

**生成しないもの**
- 要件定義、ビジネス要件（コードに書いてない）
- リスク評価、SLA（推測になる）

プロンプトに「コードに根拠がないものは書くな。推測するぐらいなら省略しろ」と明記してある。「読み取れませんでした」みたいな文言も書かせない。無いものは無い。

## 出力形式

GitHub Wiki互換で出力される。

```
wiki-output/repo/
  Home.md           # トップページ（目次）
  _Sidebar.md       # サイドバーナビゲーション
  System-Architecture.md
  API-Specification.md
  Data-Model.md
  ...
```

`{repo}.wiki.git` にそのまま `git push` できる。ページ間リンクも `[System Architecture](System-Architecture)` 形式で維持される。

## ハマったところ

### claude -pの出力にコメントが混ざる

最初は `claude -p` のstdoutをそのまま `.md` ファイルに保存してたけど、「了解しました。wikiページを作成しますね。」みたいなコメントが混ざる。

解決策：プロンプトで「Writeツールを使って直接ファイルに書け」と指示して、Go側はファイルの存在確認だけする方式に変えた。stdoutは無視。

### SSH vs HTTPS vs gh

Git cloneの認証方式で無駄に時間を使った。最終的に「GITHUB_TOKENがあればHTTPS、なければSSH」のシンプルな分岐に落ち着いた。

### 京都弁

Claude Codeのセッション設定が「京都弁で話す」になってたせいで、生成されたドキュメントが全部京都弁になった。「このAPIはPOSTリクエストを受け付けますえ」みたいな。プロンプトに「方言・口語表現禁止」を追加して解決。

## リポジトリ

https://github.com/tomohiro-owada/wikigen

Go 1.22+、git、Claude Code CLIがあれば動きます。

```bash
git clone https://github.com/tomohiro-owada/wikigen.git
cd wikigen
go build -o wikigen .
./wikigen owner/repo
```
