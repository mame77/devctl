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
devctl status       # 起動中を表示
devctl kill --all   # 全停止
devctl init         # カレントに .devctl.toml を生成
```

### Keys (TUI)

| Key | Action |
|-----|--------|
| `j` / `k` | move |
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
