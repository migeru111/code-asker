# code-asker

iPad (Safari / ホーム画面 PWA) で OSS のコードを**読む**ための、読み取り専用コードビューア。
Raspberry Pi 5 上で動かし、将来は表示中の位置情報つきで AI（`claude -p`）に質問できるようにする。

> 現在は **段階 (d)**: ファイル閲覧・シンボル一覧/ジャンプ・全文検索・AI チャット（見ている位置を自動添付）・音声での位置指定。

## セキュリティ前提（必読）
- **LAN / Tailscale 内だけで使う前提**です。インターネットには公開しないでください（TLS なし・固定トークン認証のみ）。
- `CODE_ASKER_LISTEN` は LAN または Tailscale の IP（例 `100.x.y.z:8080`）を指定します。`0.0.0.0` はファイアウォールで守られている場合のみ。
- 認証: `CODE_ASKER_TOKEN`（8文字以上）でログイン → HttpOnly Cookie。トークンなしでは `/api/*` は常に 401（`/healthz` と静的シェルを除く）。ログイン失敗は IP ごとに 5 回/分で制限。
- パスは閲覧ルート配下に正規化し、`..`・シンボリックリンク・`.git` への脱出を拒否します。ルート外を指すシンボリックリンクのリポジトリは表示されません。
- 外部コマンド（`rg` / `ctags`）は引数配列で起動し、シェルを経由しません。検索語は `-e` で渡すので `-` 始まりでもオプション扱いになりません。検索はシンボリックリンクを辿らず、結果は 200 件・10 秒で打ち切ります。

## 設定（環境変数）
| 変数 | 必須 | 説明 |
|---|---|---|
| `CODE_ASKER_ROOT` | ✔ | 閲覧ルート（例 `~/src`）。直下の git リポジトリが一覧に出る |
| `CODE_ASKER_TOKEN` | ✔ | ログイン用の固定トークン |
| `CODE_ASKER_LISTEN` | | 待受アドレス。既定 `127.0.0.1:8080` |
| `CODE_ASKER_AI` | | `off` で AI チャットを無効化 |
| `CODE_ASKER_CLAUDE_BIN` | | `claude` の場所。既定は PATH から探す |
| `CODE_ASKER_CLAUDE_MODEL` | | `claude --model` に渡す値（例 `sonnet`）。未指定ならデフォルトモデル |
| `CODE_ASKER_AI_TIMEOUT` | | 1 回の質問の制限時間。既定 `3m` |

## ローカルで試す
```sh
CODE_ASKER_ROOT=~/src CODE_ASKER_TOKEN=devtoken123 go run ./cmd/code-asker
# → http://127.0.0.1:8080
go test ./...
```

## ラズパイへの導入（初回）
前提: Go（1.22 以上）、git、curl、ripgrep（検索）、universal-ctags（シンボル一覧。無くても簡易な正規表現抽出で動くが精度は落ちる）。
```sh
sudo apt install ripgrep universal-ctags
```
```sh
git clone https://github.com/migeru111/code-asker ~/code-asker && cd ~/code-asker

# 設定ファイルと systemd ユニットを、今のユーザー・ホーム・場所に合わせて作る（sudo は付けない）
deploy/install.sh --listen 100.x.y.z:8080 --root ~/src

# 初回ビルドと起動
deploy/deploy.sh
```
- `--listen` は、ラズパイの **Tailscale / LAN の IP** とポート（例 `100.64.0.9:8080`）。省略すると `127.0.0.1:8080`（ラズパイ自身からしか開けない）。
- `--root` は、`git clone` 済みのリポジトリを置いているフォルダ（既定 `~/src`）。
- `install.sh` は **ユーザー名を決め打ちしません**（`id -un` と `$HOME`、この checkout の場所を使います）。
  - `/etc/code-asker.env`（`chmod 600`、ランダムなトークン入り）を作ります。既にあれば**上書きしません**。
  - `/etc/systemd/system/code-asker.service` を作って `enable` します。`claude` / `node` の場所を `PATH` に入れます。
  - 作られるユニットの内容は `deploy/install.sh --dry-run` で確認できます。
- 表示された**ログイン用トークン**を控えてください（あとで `sudo grep TOKEN /etc/code-asker.env` でも確認できます）。
- `deploy.sh` は `sudo systemctl restart` を使うため、パスワードなしで実行できるよう sudoers に次の 1 行を足しておくと便利です（`sudo visudo -f /etc/sudoers.d/code-asker`）。`<ユーザー名>` は `whoami` の結果に置き換えてください。
  `<ユーザー名> ALL=(root) NOPASSWD: /usr/bin/systemctl restart code-asker, /usr/bin/journalctl -u code-asker *`

### 起動しないとき
`sudo journalctl -u code-asker -n 30 --no-pager` を見ます。

| ログ | 原因と対処 |
|---|---|
| `status=217/USER` / `Failed to determine user credentials` | ユニットの `User=` が存在しない。`deploy/install.sh` をもう一度実行（ユニットを作り直す） |
| `status=203/EXEC` / `No such file` | `bin/code-asker` が無い。`deploy/deploy.sh` のビルドが成功しているか確認 |
| `CODE_ASKER_ROOT and CODE_ASKER_TOKEN must be set` / `root:` エラー | `/etc/code-asker.env` の内容、`CODE_ASKER_ROOT` のフォルダが実在するか |
| `listen tcp ...: cannot assign requested address` | `CODE_ASKER_LISTEN` の IP がこのラズパイのものではない（Tailscale が起動していない場合も） |

## AI チャットの準備（ラズパイ側）
AI は **ラズパイ上で** `claude -p` を呼びます。開発環境のログインとは別なので、ラズパイで一度だけ準備します。
```sh
# 1. Claude Code を、サービスを動かすユーザー（例 pi）で入れてログインする（Claude Pro のサブスク）
npm install -g @anthropic-ai/claude-code      # npm の prefix をユーザー領域にしておくと sudo 不要
claude                                         # 対話で /login。ログイン後 /exit
# 2. そのユーザーで非対話モードが動くことを確認
cd ~/src/<どれかのリポジトリ> && claude -p "このリポジトリの概要を1文で" --tools "Read,Grep,Glob" --permission-mode dontAsk
# 3. claude の場所は deploy/install.sh が systemd の PATH に入れる。claude を入れる/移す前に install.sh を実行していたら、もう一度 deploy/install.sh を実行する（既存の設定ファイルは上書きされない）
```
- **API キーは使いません。** サービスは子プロセスの環境から `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` を取り除いて起動するので、サブスクの枠で動きます（質問ごとに利用枠を消費します）。
- systemd から動かすため、ユニットの `HOME` は **ログインしたユーザーのホーム**にしてください（認証情報は `~/.claude` にあります）。
- AI に渡る権限は読み取りのみです: `--tools Read,Grep,Glob`（Bash/Write/Edit/Web ツール自体を持たない）、`--permission-mode dontAsk`（許可されていない操作は確認なしで拒否）、`--setting-sources ""`（閲覧中のリポジトリが持つ設定・許可ルールを読まない）、MCP とスキルは無効、作業ディレクトリは閲覧中のリポジトリのルート。ルート外のファイルや、ルート外を指すシンボリックリンクの Read は拒否されることを確認済みです。
- 1 回の質問に付くコードは「見えている行範囲（最大 300 行）＋前後 40 行」（最大 24KB）だけで、ファイル全体は送りません。それ以外はAIが必要に応じて Read/Grep で読みます。
- 質問は同時に 1 件だけ処理します（実行中に送ると「処理中です」と出ます）。

## 音声での位置指定
音声入力は iPad キーボードのマイクボタンを使います（アプリ側に音声認識はありません）。質問文の言い方から、AI に渡す位置を自動で決めます。入力中、チャット上部に「どう解釈されるか」が表示されます。

| 言い方の例 | 解釈 |
|---|---|
| 「120行目から、この処理は何をしてる？」 | L120 から約 50 行。コードをその行へスクロールし、範囲を印で表示 |
| 「120行目から150行目まで」「120〜150行」 | その範囲 |
| 「120行目」 | 前 10 行・後 30 行 |
| 「この関数は？」「この処理は？」「この構造体は？」 | 画面中央にある関数（型）を、シンボルの範囲として送る |
| 「画面の上のほう」「下のほう」「真ん中あたり」 | 見えている範囲の上・下・中央の 1/3 |
| 「parse_header という関数」 | リポジトリ内のシンボルを曖昧一致（`parse header` / `parseHeader` も同じ扱い）で検索。1 件に決まれば即送信、複数なら**候補を出して確認** |
| 「ログを書き出しているところは？」 | 位置は付けず、AI が Grep / Read で探す（探索モード） |
| 上記のどれでもない質問 | 画面に見えている範囲 |

- 全角数字・漢数字（「百二十行目」）も解釈します。行番号が指定されていれば、他の言い方より優先します。
- 解釈の本体は `web/static/locate.js`（純粋関数）で、`node --test web/tests/locate.test.js`（`go test ./...` からも実行）でテストしています。

## 更新（1コマンド）
```sh
~/code-asker/deploy/deploy.sh
```
`git pull --ff-only` → ビルド → バイナリ差し替え → `systemctl restart` → `/healthz` 確認。
ビルド失敗時は旧バイナリのまま再起動しません。ヘルスチェック失敗時は旧バイナリに戻して再起動します。

### ビルドが重い場合: arm64 クロスコンパイル
開発機で:
```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o code-asker ./cmd/code-asker
scp code-asker pi@raspberrypi:~/code-asker/bin/code-asker.new
```
ラズパイで `mv bin/code-asker.new bin/code-asker && sudo systemctl restart code-asker`。
（通常は依存ゼロの Go なので、ラズパイ上のビルドは数十秒程度です。）

## 構成と技術選定
- **Go 単一バイナリ**（標準ライブラリのみ）。フロントは `go:embed` で同梱、npm / bundler 不要。
- **ハイライト: highlight.js**（`web/static/vendor/`、同梱）。tree-sitter WASM は精度が高い反面、言語ごとの wasm 読み込みで重くなる。
  「読む」用途には軽さを優先。500 行単位のチャンクで描画するので巨大ファイルでも軽い。
- コード表示は `<div>/<span>` のみで、`textarea` / `contenteditable` / `input` を使わない（フォーカス対象がないのでキーボードが出ない）。

## 動作確認チェックリスト（iPad）
1. Safari で `http://<ラズパイのIP>:8080` を開き、トークンでログインできる
2. 共有 → 「ホーム画面に追加」→ PWA として起動できる
3. リポジトリ一覧 → ディレクトリ → ファイルと辿れ、戻るボタンが効く
4. 行番号・ハイライトが出る。長いファイルで下までスクロールすると続きが読み込まれる
5. **コード部分をタップ・長押し・スクロールしても、キーボードが出ない**（ログイン画面のトークン欄以外で出たら不具合）
6. `A−` / `A+` で文字サイズが変わり、`⏎` で折り返しが切り替わる（再読み込み後も保持）
7. ファイル表示で `{ }` を押すと下からシンボル一覧が出て、タップするとその行へジャンプしハイライトされる（キーボードは出ない）
8. 🔍 から検索画面を開く（**ここだけは入力欄にキーボードが出て正常**）。結果をタップすると該当行が開き、戻るボタンで検索結果に戻れる
9. 💬 を押すとチャットが下半分に開き、上半分のコードが見えたまま。ヘッダーに「📍 ファイル L開始–終了」が出て、スクロールすると追従する
10. 「120行目から、この処理は何をしてる？」のように質問すると、回答がストリーミングで出る。回答中の `path:行番号` をタップするとその行へ飛ぶ。続けて質問すると文脈が引き継がれ、「新規」で会話をリセットできる
11. 質問入力欄をタップしたときだけキーボードが出る。**送信後はキーボードが閉じ、コード部分では出ない**
12. 音声入力（キーボードのマイク）で「120行目から、この処理は何をしてる？」と話す。チャット上部が「📍 L120–169（行番号の指定）」になり、送信するとコードがその行へスクロールして範囲に印が付く
13. 「画面の上のほうは何？」「この関数は何をしてる？」が、見えている範囲を基準に解釈される
14. 「（関数名）という関数は？」で、同名が複数あれば候補が出て、タップで選べる（候補が 1 件に決まれば即送信）
15. 「ログを書き出しているところは？」で、位置を付けずに AI が探して `path:行番号` を答える
16. 再起動後の自動起動: `sudo reboot` 後にアクセスできる
