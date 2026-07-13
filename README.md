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
devctl jump         # built-in TUI で選んだ path を出力
cd "$(devctl jump)" # 選んだリポジトリへ移動
devctl jump --tmux  # tmux セッションへ移動
devctl status       # 起動中を表示
devctl kill --all   # 全停止
devctl init         # カレントに .devctl.toml を生成
devctl scan         # discovered cache を更新
```

### Shell binding (Ctrl+G)

`~/.bashrc` のリポジトリ移動キーバインドの代わりに:

```bash
bind -x '"\C-g": cd "$($HOME/go/bin/devctl jump)"'
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
| `j` / `k` | move (list or ports panel) |
| `Tab` / `l` / `h` | focus ports panel / list |
| `Ctrl+P` | toggle key help |
| `/` | filter by repository name |
| `Esc` | close help / clear filter / leave ports focus |
| `e` | edit repo config under `~/.config/devctl/projects/` |
| `Enter` / `g` | print selected project path |
| `Ctrl+G` | start search |
| `Space` | start / switch (kills others with ports) |
| `o` | open primary port in browser |
| `x` | kill selected (list or ports panel) |
| `a` | kill all |
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
State/logs: `~/.local/state/devctl/`
