# devbox / dev

npm / PyPI などのサプライチェーン攻撃や悪意あるリポジトリから、ホストの鍵を守るための CLI です。

- プロジェクトのコードを動かすもの（Neovim の LSP、Claude Code、npm / cargo / go / composer、テスト、開発サーバー）は、**すべてコンテナの中**で動かします。
- 鍵（1Password、SSH、AWS）と `git commit` / `git push` は**ホストに残します**。
- コンテナには、そのプロジェクトのフォルダと、明示的に許可したものだけを渡します。

仕様と設計判断は `docs/superpowers/specs/` にあります。

## インストール

Go 1.27.4 を asdf で入れてから、ビルドしてインストールします。

```sh
asdf install golang 1.27.4    # .tool-versions で固定しています
make test
make install                  # ~/.local/bin/dev に置きます
dev build                     # devbox:latest イメージを作ります
```

`dev` は標準ライブラリだけで書かれた 1 つのバイナリです。`image/`（Dockerfile と devbox-check）はバイナリに埋め込まれているので、インストール後はこのリポジトリがなくても `dev build` できます。

## シェル版からの移行

`make install` で `~/.local/bin/dev` を置き換えるだけです。

- 今の `~/.local/bin/dev` はシェル版へのシンボリックリンクです。`make install` はリンクを消してから実体を置くので、リンク先の `dev.sh` は書き換わりません。
- **設定**（`.devbox`、`~/.config/devbox/`）と **`.devbox` の承認記録**（`~/.local/state/devbox/approved/`）は、同じ形式なのでそのまま使えます。
- **シェル版で作ったコンテナ**はマウントの配置が違うので、使えません。入ろうとすると「古い dev で作られています」と表示されるので、`dev stop` してから `dev` で作り直してください。共有ボリューム（`devbox-nvim-data` など）はそのまま使われます。

## 使い方

cmux のペインを開き、プロジェクトのフォルダで実行します。2 つ目以降のペインは、同じコンテナに入ります。

```sh
dev nvim .        # エディタ
dev claude        # Claude Code
dev               # シェル（開発サーバーなど）
```

| コマンド | 動作 |
|---|---|
| `dev` | プロジェクトのコンテナに入る（なければ作る）。`bash -l` |
| `dev <command...>` | コンテナ内で `<command...>` を実行 |
| `dev -- <command...>` | `stop` や `build` など、サブコマンドと同名のコマンドをコンテナ内で実行 |
| `dev stop` | このプロジェクトのコンテナを削除（設定を変えたら一度これ） |
| `dev ls` | devbox のコンテナの一覧 |
| `dev check` | 隔離が効いているか、コンテナ内で確認 |
| `dev build [args...]` | イメージを作る（ツールの更新もこれ）。引数は `docker build` へ |
| `dev help` | 使い方 |

- プロジェクトは、git リポジトリの最上位です（git でなければカレントディレクトリ）。サブフォルダで実行すると、コンテナ内でも同じサブフォルダで動きます。
- ホームディレクトリそのものや、`/`、`/Users` では動きません（ホーム全体をマウントしないため）。`~/.ssh`、`~/.aws`、`~/.config`、`~/Library` などの中でも動きません。
- git リポジトリではないフォルダで、中に別のリポジトリがある場所（`~/src` など）では動きません。リポジトリのフォルダで実行してください。
- モノレポでは、どのサブフォルダから実行しても、リポジトリ全体が 1 つのコンテナになります。git submodule や git worktree の中では、コンテナ内の git は使えません（ホストの git は使えます）。
- 環境変数 `DEVBOX_IMAGE` でイメージ名（既定 `devbox:latest`）、`DEVBOX_CONTEXT` で `dev build` のビルドコンテキストを変えられます。

### コンテナに渡すもの

| ファイル | 内容 |
|---|---|
| `<project>/.devbox` | このプロジェクトに渡す環境変数名とポート。**内容が変わるたびに確認されます** |
| `<project>/.env` | 値（1Password の `op://` 参照） |
| `~/.config/devbox/devbox` | 全プロジェクト共通の環境変数名とポート（確認なし） |
| `~/.config/devbox/secrets.env` | 全プロジェクト共通の値（`op://` 参照） |

```
# .devbox / devbox の書式（シェルとして実行はされません）
env DATABASE_URL     # ホストの環境変数を同じ名前で渡す。値は書かない
port 5173            # 127.0.0.1:5173 -> コンテナ 5173
port 8080:80         # 127.0.0.1:8080 -> コンテナ 80
```

- `.env` や `secrets.env` に `op://` があり、`op` が入っていれば、`op run` 経由で値を読み込みます。値がコマンドラインに載ることはありません。
- `.env` に書けるのは、`.devbox` か全体設定で `env` として列挙した名前だけです。`DOCKER_*`、`PATH`、`HOME`、`XDG_*`、`GIT_*` などの、dev や docker の動作を変えられる名前は書けません。
- `.devbox` と `.env` は、内容が変わるたびに確認されます。確認の画面には、`.env` の名前ごとに 1Password の参照先が表示されます。
- `.devbox` と `.env` は、通常のファイルにしてください（シンボリックリンクは読みません）。
- 設定はコンテナを**作るとき**にだけ使われます。変えたら `dev stop` してから `dev` を実行してください。
- ひな形は `examples/` にあります。`examples/env.example` は、プロジェクト直下に `.env` という名前でコピーして使います（`.env` はコミットされないように `.gitignore` で除外しています）。Neovim（LazyVim）をコンテナで使うための設定は `examples/nvim-devbox.lua` を見てください。

## このリポジトリを開発するときの注意

`dev` は、ホストで動く**唯一の信頼された道具**です。1Password と docker を操作します。ほかのプロジェクトのコードはコンテナに閉じ込めますが、このリポジトリのコードはホストで実行されます。

- このリポジトリをコンテナの中（`dev nvim .` や `dev claude`）で編集した場合は、**ホストで `git diff` を確認してから** `make build` / `make install` してください。コンテナの中で書き換えられたコードが、そのままホストで動くことになるためです。
- 外部の Go モジュールは追加しません（追加すると、そのコードがホストで動くため）。
- テストは Docker・git・op・端末がなくても動きます。

```sh
make test     # go test ./...
make vet      # go vet ./...
make fmt      # gofmt の確認
make build    # bin/dev
```
