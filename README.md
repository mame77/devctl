# devctl

複数リポジトリを移動しサーバーを起動 停止する TUI。

リポジトリで設定したコマンドを簡単に実行

## Install

```bash
cd ~/ghq/github.com/mame77/devctl
go install .
```

## Usage

```bash
devctl              # TUI
devctl shell zsh    # parent shell を cd できるラッパー関数を出力
devctl jump         # built-in TUI で選んだ path へ移動 (shell integration 有効時)
devctl jump --tmux  # tmux セッションへ移動
devctl status       # 起動中を表示
devctl kill --all   # 全停止
devctl init         # カレントに .devctl.toml を生成
devctl scan         # discovered cache を更新
```

### Shell integration

`devctl` から親シェルのカレントディレクトリを直接変更することはできません。
リポジトリ移動を有効にするには、シェル起動ファイルにラッパー関数を追加します。

zsh:

```zsh
eval "$(devctl shell zsh)"
```

bash:

```bash
eval "$(devctl shell bash)"
```

ラッパーは `devctl --cwd-file <tmp>` で選択結果を一時ファイルに書かせ、
TUI 終了後にシェル関数側で `cd` します。
スクリプトで path を stdout から受け取りたい場合は、関数を避けて
`command devctl jump <path>` を使ってください。

### Shell binding (Ctrl+G)

shell integration を有効にした後で、必要ならキーに割り当てます。

```bash
bind -x '"\C-g": devctl jump'
```

### tmux popup (`prefix+d`)

tmux 連携はオプションです。popup 内で tmux jump したい場合だけ、
次のように **popup 後に pending を適用**します:

```tmux
bind d run-shell 'tmux display-popup -d "#{pane_current_path}" -E -w 100% -h 100% "env DEVCTL_POPUP=1 $HOME/go/bin/devctl jump --tmux"; $HOME/go/bin/devctl jump --apply-pending'
```

`nix-config` の `dotfiles/tmux/tmux.conf` には既に反映済み。`home-manager switch` 後に有効。
### Keys (TUI)

| Key | Action |
|-----|--------|
| `j` / `k` | move |
| `Ctrl+P` | toggle key help |
| `/` | filter by repository name |
| `Esc` | close help / clear filter |
| `e` | edit repo config under `~/.config/devctl/projects/` |
| `Enter` / `g` | jump to selected project path |
| `Ctrl+G` | start search |
| `Space` | start / kill toggle |
| `o` | open primary port in browser |
| `x` | kill selected |
| `a` | kill all |
| `p` | pin / unpin selected repository |
| `r` | rescan repositories |
| `q` | quit TUI (dev keeps running) |

## Config

## Project discovery

1. Explicit `[[projects]]` in config (highest priority)
2. Walk `scan_roots` for `.git` roots (no `ghq` dependency)

Each list item is a **repo root** only (no monorepo subdirs).

`scan_roots` defaults to existing developer folders such as `~/ghq`, `~/src`,
`~/dev`, `~/projects`, `~/code`, `~/work`, and `~/repos`. Hidden directories
and `node_modules`/`vendor`/`dist`/… are skipped. If you explicitly set
`scan_roots = ["~"]`, devctl also skips OS-specific heavy directories such as
`~/Library` on macOS as a safety guard.

Discovered repositories are cached under `~/.local/state/devctl/cache/`.
`devctl` reads that cache on startup and does not rescan automatically unless
the cache does not exist yet. Run `devctl scan` or press `r` in the TUI to
refresh it.

**Command is manual.** Space start works only when you set `command`.

Per-repo config (priority high → low):

1. `<repo>/.devctl.toml`
2. `~/.config/devctl/projects/<ghq-relative>.toml`  
   例: `~/.config/devctl/projects/github.com/mame77/devctl.toml`
3. global `[[projects]]` in `config.toml`

`e` は常に **2**（`~/.config/devctl/projects/...`）を開く／無ければそこに作成（repo には書かない）。  
実行時の読み込みは **1 が優先**。

### Global `~/.config/devctl/config.toml`

```toml
scan_roots = ["~/ghq", "~/src"]  # optional; defaults to existing dev folders
scan_depth = 4

# Temporarily hide repositories (ghq-relative or absolute paths).
# Comment out to show them again.
ignore = ["github.com/digeon-inc"]

[[projects]]
name = "goal-share"
path = "/home/mame/ghq/github.com/Webu-Kobedenshi/goal-share"
command = "npm run dev"
port = 3000
```

### Per-repo (XDG)

```toml
# ~/.config/devctl/projects/github.com/digeon-inc/jal-eap.toml
name = "jal-eap"
command = "npm run dev --prefix app"
ports = [3000, 8787]   # 先頭が UI（`o` で開く / 表示の primary）
# port = 3000          # 単一ポートならこちらでも可
```

### Per-repo (in repository, highest priority)

```toml
# <repo>/.devctl.toml
name = "jal-eap"
command = "npm run dev --prefix app"
ports = [3000, 8787]   # first = UI port for `o` and display
```
### Pin repositories

`p` key in TUI toggles a pin on the selected repository. Pinned repositories
stay at the top of the list. The pin list is stored in
`~/.local/state/devctl/pins.json`.

State/logs: `~/.local/state/devctl/`
