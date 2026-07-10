# devctl

複数リポジトリの dev サーバーを TUI で切替・停止する CLI。

同時に起動できる dev は **常に1つ**。別プロジェクトを起動すると既存は自動で kill される。

## Install

```bash
cd ~/ghq/github.com/mame77/devctl
go install .
```

## Usage

```bash
devctl              # TUI
devctl jump         # fzf で ghq リポジトリを選び tmux セッションへ (Ctrl+G 相当)
devctl jump <path>  # 指定 path へ直接 jump
devctl status       # 起動中を表示
devctl kill --all   # 全停止
devctl init         # カレントに .devctl.toml を生成
```

### Shell binding (Ctrl+G)

`~/.bashrc` の `projects-fzf` の代わりに:

```bash
bind -x '"\C-g": devctl jump'
```

### tmux popup (`prefix+d`)

popup 内で Enter jump すると、popup 終了時に元セッションへ戻る。  
次のように **popup 後に pending を適用**する:

```tmux
bind d run-shell 'tmux display-popup -d "#{pane_current_path}" -E -w 100% -h 100% "env DEVCTL_POPUP=1 $HOME/go/bin/devctl"; $HOME/go/bin/devctl jump --apply-pending'
```

`nix-config` の `dotfiles/tmux/tmux.conf` には既に反映済み。`home-manager switch` 後に有効。
### Keys (TUI)

| Key | Action |
|-----|--------|
| `j` / `k` | move |
| `/` | filter by repository name |
| `Esc` | clear filter (or exit search input) |
| `Enter` / `g` | jump to selected project (tmux) |
| `Ctrl+G` | fzf pick any ghq repo → jump |
| `Space` | start / switch (kills others) |
| `x` | kill selected |
| `a` | kill all |
| `r` | reload |
| `q` | quit TUI (dev keeps running) |

## Config

## Project discovery

1. Explicit `[[projects]]` in config (highest priority)
2. `ghq list --full-path` when `ghq` is available
3. Otherwise walk `scan_roots`

Detected automatically:

- `.devctl.toml` in a repo (or monorepo subdir)
- `package.json` with a `dev` script (lockfile から `npm`/`pnpm`/`yarn`/`bun` を推定)

Monorepo は直下 + 1階層のサブディレクトリも見る（例: `app/`, `packages/web`）。

### Global `~/.config/devctl/config.toml`

```toml
default_command = "npm run dev"
# ghq がないときのフォールバック
scan_roots = ["~/ghq"]
scan_depth = 6

[[projects]]
name = "goal-share"
path = "/home/mame/ghq/github.com/Webu-Kobedenshi/goal-share"
command = "npm run dev"
port = 3000
```

### Project `.devctl.toml`

```toml
name = "my-app"
command = "npm run dev"
port = 3000
```

State/logs: `~/.local/state/devctl/`
