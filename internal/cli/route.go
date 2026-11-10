package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperme-sh/hypermesh-cli/internal/route"
)

func newRouteCmd(r *run) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Detect local AI tools and optionally point them at Conductor",
		Long: `Read-only detect lists AI CLIs and editors and whether each one has an OpenAI-compatible or Anthropic-compatible base URL.

enable and disable are opt-in. enable writes that tool's own config, keeps the renter key as a reference, and stores a backup under the Hypermesh config directory. disable restores that backup. --dry-run prints the diff and writes nothing.

Conductor serves POST /v1/messages, so Anthropic clients can be pointed at the conductor origin. Tools without a documented base URL are reported and left unchanged.`,
	}
	cmd.AddCommand(newRouteDetectCmd(r))
	cmd.AddCommand(newRouteEnableCmd(r))
	cmd.AddCommand(newRouteDisableCmd(r))
	cmd.AddCommand(newRouteStatusCmd(r))
	return cmd
}

func newRouteDetectCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Scan this machine for AI tools and their model endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := routeOptions(r)
			if err != nil {
				return err
			}
			found, err := route.Detect(opts)
			if err != nil {
				return err
			}
			return printFindings(cmd.OutOrStdout(), r.json, found)
		},
	}
}

func newRouteStatusCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show tools currently pointed at Conductor",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := routeOptions(r)
			if err != nil {
				return err
			}
			found, err := route.Status(opts)
			if err != nil {
				return err
			}
			return printFindings(cmd.OutOrStdout(), r.json, found)
		},
	}
}

func newRouteEnableCmd(r *run) *cobra.Command {
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "enable [tool|--all]",
		Short: "Point a tool's own config at Conductor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRouteChange(r, cmd, args, all, dryRun, true)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "enable every detected tool that has a supported base URL")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the diff and write nothing")
	return cmd
}

func newRouteDisableCmd(r *run) *cobra.Command {
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "disable [tool|--all]",
		Short: "Restore a tool from the backup taken when it was enabled",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRouteChange(r, cmd, args, all, dryRun, false)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "restore every tool this CLI is managing")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the restore diff and write nothing")
	return cmd
}

func runRouteChange(r *run, cmd *cobra.Command, args []string, all, dryRun, enable bool) error {
	if all == (len(args) == 1) {
		return fmt.Errorf("pass a tool id or --all")
	}
	opts, err := routeOptions(r)
	if err != nil {
		return err
	}
	var results []route.Result
	if all {
		if enable {
			results, err = route.EnableAll(opts, dryRun)
		} else {
			results, err = route.DisableAll(opts, dryRun)
		}
	} else {
		var result route.Result
		if enable {
			result, err = route.Enable(opts, args[0], dryRun)
		} else {
			result, err = route.Disable(opts, args[0], dryRun)
		}
		results = []route.Result{result}
	}
	if printErr := printResults(cmd.OutOrStdout(), r.json, results); printErr != nil {
		return printErr
	}
	return err
}

func routeOptions(r *run) (route.Options, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return route.Options{}, fmt.Errorf("home directory is required")
	}
	return route.Options{
		Home:          home,
		ConfigDir:     r.cfg.Dir,
		ConductorBase: r.cfg.ConductorBase,
		Path:          os.Getenv("PATH"),
		Environ:       os.Environ(),
		GOOS:          runtime.GOOS,
	}, nil
}

func printFindings(w io.Writer, asJSON bool, found []route.Finding) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(found)
	}
	fmt.Fprintln(w, "tool\tdetected\tprotocol\tsupported\tredirected\tendpoint\tconfig")
	for _, row := range found {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ID, yesNo(row.Detected), row.Protocol, yesNo(row.Supported), yesNo(row.Redirected), row.Endpoint, row.ConfigPath)
	}
	return nil
}

func printResults(w io.Writer, asJSON bool, results []route.Result) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	for _, result := range results {
		fmt.Fprintf(w, "%s\t%s\n", result.Tool, result.Action)
		if result.Notes != "" {
			fmt.Fprintf(w, "  %s\n", result.Notes)
		}
		for _, change := range result.Changes {
			fmt.Fprintf(w, "  %s\n", change.Path)
			if change.Diff != "" {
				fmt.Fprintln(w, change.Diff)
			}
		}
	}
	return nil
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
