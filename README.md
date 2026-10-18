<p align="center">
  <img src="docs/assets/logo.png" alt="Arkadia のロゴ" width="160">
</p>

<h1 align="center">Arkadia Lang</h1>

**Arkadia** は、代数的エフェクトと型クラスを備えた、正格評価の静的型付き関数型プログラミング言語です。
ネイティブバイナリにコンパイルされ、構造化された軽量タスクがすべてのプロセッサで並列に動きます。

```haskell
import Std.State (..)
import Std.Task (parMap)

-- エフェクトは型に現れ、ハンドラ（runState）が処理する
counter : Unit -> <State Int, IO> Unit
counter () =
  let n = get ()
  println "count = ${n}"
  put (n + 1)

-- 制御は match・ガード・再帰だけ
fib : Int -> Int
fib n
  | n < 2 = n
  | otherwise = fib (n - 1) + fib (n - 2)

main () =
  let (_, final) = runState 0 (\_ -> counter (); counter ())
  println "final = ${final}"
  println "${parMap fib [25, 30, 35]}"   -- 各要素を別のタスクで並列に計算
```

> [!NOTE]
> Arkadia は 0.x の開発段階です。1.0 になるまでは、マイナーバージョンの更新で互換性のない変更が入ることがあります。

## 特徴

- **代数的エフェクト** — 例外・状態・ジェネレータなどをライブラリとして定義できます。エフェクトは行多相の型で追跡・推論され、処理されていないエフェクトはコンパイルエラーになります。
- **構造化された並列実行** — タスクは全プロセッサで並列に動き、キャンセル・タイムアウト・失敗はスコープに沿って伝播します。純粋な無限ループも外からキャンセルできます。
- **表現力のある型システム** — 型クラス（高カインド・`deriving`）、レコード、GADT と型族によるインデックス型。網羅性はコンパイル時に検査されます。
- **小さな構文** — `if` もループも無く、制御は `match`・ガード・再帰だけ。インデントによるブロック、空白区切りの関数適用、全関数カリー化。
- **ネイティブバイナリ** — Go を経由して単一の静的バイナリを生成し、Go ランタイムの GC とスケジューラの上で動きます。Go の関数も呼び出せます（FFI）。
- **ツール一式** — ビルド・実行・テスト・フォーマッタ・ドキュメント・REPL・依存管理を `ark` コマンドひとつで。Language Server と VS Code 拡張もあります。
## インストール

Linux と macOS（amd64・arm64）に対応しています。`ark` は Go のコードを生成して `go build` するので、**Go 1.22 以降**が必要です。

[Releases](https://github.com/arkadia-lang/arkadia/releases) から環境に合ったアーカイブを取得して展開し、`bin/` を `PATH` に加えます。

```sh
curl -LO https://github.com/arkadia-lang/arkadia/releases/download/v0.1.0/arkadia-0.1.0-linux-amd64.tar.gz
tar xzf arkadia-0.1.0-linux-amd64.tar.gz
export PATH="$PWD/arkadia-0.1.0-linux-amd64/bin:$PATH"
ark version
```

アーカイブには `ark` 本体と、標準ライブラリ（`lib/`）、Go ランタイム（`runtime/rt/`）が入っています。展開したディレクトリの中の配置は変えずに使ってください。

ソースからビルドする方法は [開発](#開発) を参照してください。

## はじめる

```sh
ark new hello      # プロジェクトを作成
cd hello
ark run            # ビルドして実行  → Hello, Arkadia!
ark test           # テストを実行
ark build          # bin/hello を生成
```

単一のファイルも直接実行できます（`ark run main.ark`）。

主なコマンド:

| コマンド | 説明 |
|---|---|
| `ark new <name>` | プロジェクトの雛形を作成 |
| `ark run [file] [-- args]` | ビルドして実行 |
| `ark build [file]` | ネイティブバイナリを生成 |
| `ark check [file]` | 型検査のみ |
| `ark test [file]` | テストを実行 |
| `ark fmt [path...]` | ソースを整形 |
| `ark doc <Module>` | モジュールの公開 API を表示（例: `ark doc Std.Map`） |
| `ark repl` | 対話環境 |
| `ark lsp` | Language Server（VS Code 拡張は [`editors/vscode`](editors/vscode)） |

## ドキュメント

- [言語ガイド](docs/guide.md) — 構文・型・型クラス・エフェクト・タスク・テスト・FFI のリファレンス
- 標準ライブラリ — `ark doc <Module>` で各モジュールの API とドキュメントを表示できます
- [変更履歴](CHANGELOG.md)

## サンプル

- [`examples/parallel.ark`](examples/parallel.ark) — タスクによるデータ並列・チャネルのパイプライン・純粋なループのキャンセル
- [`examples/scheduler.ark`](examples/scheduler.ark) — エフェクトで実装した協調スレッド
- [`examples/json.ark`](examples/json.ark) — State と Fail エフェクトによる JSON パーサ
- [`examples/calculator.ark`](examples/calculator.ark) — パーサコンビネータと `Result` による電卓
- [`examples/lisp.ark`](examples/lisp.ark) — クロージャ付きの小さな Lisp インタプリタ
- [`examples/http/server.ark`](examples/http/server.ark) — JSON API と静的ページの TODO アプリ

## 制限事項

- 継続はワンショットです（同じ継続を二度再開すると実行時エラー）。非決定性のような多重再開には対応していません。
- 型クラスは単一パラメータのみです。
- インデックス型にカインド多相はありません（[詳細](docs/guide.md#インデックス型)）。
- `Ref`・`MArray`・`HashMap` はタスク間で共有できません。タスク間の共有には `Std.Channel`・`Std.Atomic`・`Std.Mutex` を使います。

## 開発

コンパイラ（[`compiler/`](compiler)）は Arkadia で書かれていて、最初の一段だけを Go で書かれた凍結済みのブートストラップ用コンパイラ（[`bootstrap/`](bootstrap)）でビルドします。
ソースからのビルドには Go と [just](https://github.com/casey/just) が必要です（`nix develop` で開発環境に入れます）。

```sh
git clone https://github.com/arkadia-lang/arkadia.git && cd arkadia
just build      # bin/ark0 (Go) → bin/ark1 → bin/ark
just install    # $GOBIN/ark から bin/ark へのシンボリックリンクを張る
just test       # テストスイート
just fixpoint   # 自分自身のビルドが固定点に達することを確認
```

`ark` は標準ライブラリとランタイムを、実行ファイル（シンボリックリンクの先）の親ディレクトリか `$ARKADIA_ROOT` から探します。

| ディレクトリ | 内容 |
|---|---|
| [`compiler`](compiler) | コンパイラ `ark`（[構成](compiler/README.md)） |
| [`lib`](lib) | 標準ライブラリ |
| [`runtime/rt`](runtime/rt) | 生成コードにリンクされる Go のランタイム |
| [`bootstrap`](bootstrap) | ブートストラップ用コンパイラ（Go、[説明](bootstrap/README.md)） |
| [`tests`](tests) | テストスイート |
| [`editors/vscode`](editors/vscode) | VS Code 拡張 |

リリースの手順は [docs/releasing.md](docs/releasing.md) にあります。

## ライセンス

[MIT](LICENSE)
