package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/FyberLabs/hypermesh-cli/internal/terms"
)

func newTermsCmd() *cobra.Command {
	var accept bool
	cmd := &cobra.Command{
		Use:   "terms",
		Short: "Print the Hypermesh Terms",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			if err := terms.WriteText(w); err != nil {
				return err
			}
			fmt.Fprintf(w, "version: %s\nsha256: %s\n", terms.Version, terms.Hash())
			if !accept {
				return nil
			}
			if err := terms.Write(); err != nil {
				return err
			}
			fmt.Fprintln(w, "accepted")
			return nil
		},
	}
	cmd.Flags().BoolVar(&accept, "accept", false, "record acceptance of these Terms")
	return cmd
}

func termsExempt(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup("help"); f != nil && f.Changed {
		return true
	}
	switch cmd.Name() {
	case "help", "version", "terms":
		return true
	default:
		return false
	}
}

func gateTerms(cmd *cobra.Command, license string) error {
	if err := terms.ValidateLicense(license); err != nil {
		return err
	}
	if termsExempt(cmd) {
		if terms.WantsAccept(license) {
			return terms.RecordIfNeeded()
		}
		return nil
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	return terms.Ensure(os.Stdin, os.Stdout, interactive, license)
}
