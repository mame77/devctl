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

### Global `~/.config/devctl/config.toml`

```toml
default_command = "npm run dev"
scan_roots = ["~/ghq"]
scan_depth = 5
scan_markers = [".devctl.toml"]

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
# devctl
