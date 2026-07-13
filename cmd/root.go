package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/mame77/devctl/internal/config"
	"github.com/mame77/devctl/internal/jump"
	"github.com/mame77/devctl/internal/session"
	"github.com/mame77/devctl/internal/ui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devctl",
	Short: "TUI to manage dev servers across repositories",
	Long:  "devctl lists configured/scanned projects and lets you start/switch/kill dev processes (one at a time).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(false)
	},
}

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(false)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show running dev processes",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := session.New()
		if err != nil {
			return err
		}
		items, err := mgr.List()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPID\tPORT\tSTATUS\tPATH")
		any := false
		for _, it := range items {
			st := "stopped"
			pid := "-"
			port := "-"
			if it.Running {
				any = true
				st = "running"
				pid = fmt.Sprintf("%d", it.PID)
			}
			if len(it.Ports) > 0 {
				parts := make([]string, len(it.Ports))
				for i, p := range it.Ports {
					parts[i] = fmt.Sprintf("%d", p)
				}
				port = strings.Join(parts, ",")
			}
			if it.Running {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.Name, pid, port, st, it.Path)
			}
		}
		_ = w.Flush()
		if !any {
			fmt.Println("(none running)")
		}
		return nil
	},
}

var startCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a project (kills any other running project)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := session.New()
		if err != nil {
			return err
		}
		if err := mgr.StartSwitch(args[0]); err != nil {
			return err
		}
		fmt.Printf("started %s\n", args[0])
		return nil
	},
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan repositories and refresh the discovered cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := session.New()
		if err != nil {
			return err
		}
		if err := mgr.ReloadConfig(); err != nil {
			return err
		}
		if err := mgr.Rescan(); err != nil {
			return err
		}
		items, err := mgr.List()
		if err != nil {
			return err
		}
		fmt.Printf("scanned %d projects\n", len(items))
		return nil
	},
}

var killAll bool

var killCmd = &cobra.Command{
	Use:   "kill [name]",
	Short: "Kill a running project (or --all)",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := session.New()
		if err != nil {
			return err
		}
		if killAll {
			if err := mgr.KillAll(); err != nil {
				return err
			}
			fmt.Println("killed all")
			return nil
		}
		if len(args) < 1 {
			return fmt.Errorf("usage: devctl kill <name> | devctl kill --all")
		}
		if err := mgr.Kill(args[0]); err != nil {
			return err
		}
		fmt.Printf("killed %s\n", args[0])
		return nil
	},
}

var initLocal bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create per-repo config (default: ~/.config/devctl/projects/...)",
	Long: `Create a project config stub.

Default: ~/.config/devctl/projects/<ghq-relative>.toml
With --local: <cwd>/.devctl.toml
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if initLocal {
			path := config.RepoLocalPath(cwd)
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			if err := config.WriteProjectFile(cwd, config.ProjectFile{
				Name: filepath.Base(cwd),
			}); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", path)
			return nil
		}
		path, err := config.EnsureProjectFile(cwd, filepath.Base(cwd))
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
		return nil
	},
}

var jumpCmd = &cobra.Command{
	Use:   "jump [path]",
	Short: "Pick a repository path for cd, or jump via tmux with --tmux",
	Long: `Pick a repository from the discovered cache and print its path.
This is intended for shell wrappers such as:

  cd "$(devctl jump)"

With no args: open the built-in TUI picker.
With a path: print that path after validation.
With --tmux: create or switch to a tmux session instead.

Shell binding example (bash):
  bind -x '"\C-g": cd "$(devctl jump)"'

tmux popup (prefix+d) can still apply pending after close:
  bind d run-shell 'tmux display-popup -E -w 100% -h 100% "devctl jump --tmux"; devctl jump --apply-pending'
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if applyPending {
			return jump.ApplyPending()
		}
		if len(args) == 1 {
			if jumpTmux {
				return jump.ToTmux(args[0])
			}
			return jump.PrintPath(args[0])
		}
		return runTUI(jumpTmux)
	},
}

var applyPending bool
var jumpTmux bool

func runTUI(tmuxJump bool) error {
	mgr, err := session.New()
	if err != nil {
		return err
	}
	return ui.Run(mgr, tmuxJump)
}

func Execute() {
	killCmd.Flags().BoolVar(&killAll, "all", false, "kill all running projects")
	initCmd.Flags().BoolVar(&initLocal, "local", false, "write .devctl.toml in the current directory")
	jumpCmd.Flags().BoolVar(&applyPending, "apply-pending", false, "switch to session recorded by popup jump")
	jumpCmd.Flags().BoolVar(&jumpTmux, "tmux", false, "open or switch tmux session instead of printing the path")
	rootCmd.AddCommand(tuiCmd, statusCmd, startCmd, killCmd, initCmd, jumpCmd, scanCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
