package cli

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/hyperme-sh/hypermesh-cli/internal/api"
	"github.com/hyperme-sh/hypermesh-cli/internal/config"
)

// ExitFailure is the only non-zero status this process returns.
// Scripts can rely on it. An HTTP status is never the process status.
const ExitFailure = 1

type run struct {
	json          bool
	apiBase       string
	conductorBase string
	license       string
	cfg           config.Config
	client        *api.Client
}

func New(name string) *cobra.Command {
	r := &run{}
	root := &cobra.Command{
		Use:               name,
		Short:             "Thin Hypermesh CLI — catalog, checkout, conductor chat, and local MCP",
		Long:              "Hypermesh (Hyperme.sh) renter CLI: browse the catalog, check out a lease, send chat through Conductor, and manage local MCP for the desktop visor.\n\nAccept the Hypermesh Terms with --license accept or HYPERMESH_LICENSE=accept. hypermesh terms prints the text. Host install --yes is a separate choice.",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := gateTerms(cmd, r.license); err != nil {
				return err
			}
			if termsExempt(cmd) {
				return nil
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if r.apiBase != "" {
				cfg.APIBase = r.apiBase
			}
			if r.conductorBase != "" {
				cfg.ConductorBase = r.conductorBase
			}
			r.cfg = cfg
			r.client = api.NewClient(cfg.APIBase, cfg.ConductorBase, cfg.APIKey, cfg.TenantID)
			r.client.ConductorClass = cfg.ConductorClass
			r.client.ConductorSuggest = cfg.ConductorSuggest
			r.client.ConductorToken = os.Getenv(api.EnvConductorProxyToken)
			return nil
		},
	}
	root.PersistentFlags().BoolVar(&r.json, "json", false, "print machine-readable JSON")
	root.PersistentFlags().StringVar(&r.apiBase, "api-base", "", "override HYPERMESH_API_BASE")
	root.PersistentFlags().StringVar(&r.conductorBase, "conductor-base", "", "override HYPERMESH_CONDUCTOR_BASE (conductor origin, default http://127.0.0.1:8787)")
	root.PersistentFlags().StringVar(&r.license, "license", "", "accept the Hypermesh Terms (value: accept)")

	root.AddCommand(newAuthCmd(r))
	root.AddCommand(newCatalogCmd(r))
	root.AddCommand(newClassesCmd(r))
	root.AddCommand(newHostsCmd(r))
	root.AddCommand(newCheckoutCmd(r))
	root.AddCommand(newLeaseCmd(r))
	root.AddCommand(newChatCmd(r))
	root.AddCommand(newPromptCmd(r))
	root.AddCommand(newCompletionsCmd(r))
	root.AddCommand(newRouteCmd(r))
	root.AddCommand(newMCPCmd(r))
	root.AddCommand(newTermsCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func Execute(name string) {
	os.Exit(Run(name, os.Args[1:]))
}

// Run executes the CLI and returns the process status.
// Diagnostics go to stderr. Script chat writes model text only to stdout.
func Run(name string, args []string) int {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	cmd := New(name)
	cmd.SetArgs(args)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitFailure
	}
	return 0
}
