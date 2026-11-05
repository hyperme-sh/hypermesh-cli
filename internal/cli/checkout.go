package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperme-sh/hypermesh-cli/internal/api"
)

func newCheckoutCmd(r *run) *cobra.Command {
	var (
		catalogID     string
		renterUserID  string
		deviceID      string
		reservedHours int
		wait          bool
		pollEvery     time.Duration
		waitTimeout   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "checkout",
		Short: "Create a loaded-model renter lease",
		Long:  "POST /api/v1/hypermesh/leases with kind=p2_loaded_model, purpose=renter, catalog_id, and device_id (UUID from hosts). Missing device_id fails before POST. Prints the lease id immediately. The plane opens the USDC payment on that lease; this command does not call a checkout URL. --wait polls GET /leases/{id}.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if renterUserID == "" {
				renterUserID = r.cfg.RenterUserID
			}
			body := api.NewLeaseCreate(renterUserID, catalogID, reservedHours, deviceID)
			lease, raw, err := r.client.CreateLease(body)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "lease_id\t%s\n", lease.ID)
			if wait {
				var werr error
				lease, raw, werr = pollLease(r, lease.ID, pollEvery, waitTimeout)
				if werr != nil {
					return werr
				}
			}
			if r.json {
				return r.printRawJSON(raw)
			}
			return printLease(cmd.OutOrStdout(), lease)
		},
	}
	cmd.Flags().StringVar(&catalogID, "catalog-id", api.DefaultCatalogID, "catalog model id")
	cmd.Flags().StringVar(&deviceID, "device-id", "", "host device_id UUID from hosts (not public_label)")
	cmd.Flags().StringVar(&renterUserID, "renter-user-id", "", "renter user uuid")
	cmd.Flags().IntVar(&reservedHours, "reserved-hours", 1, "reserved hours")
	cmd.Flags().BoolVar(&wait, "wait", false, "poll GET /leases/{id} until active|failed")
	cmd.Flags().DurationVar(&pollEvery, "poll-interval", 2*time.Second, "lease poll interval with --wait")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 15*time.Minute, "give up waiting after this duration")
	return cmd
}

func printLease(w io.Writer, lease api.Lease) error {
	if _, err := fmt.Fprintf(w, "id\t%s\nstatus\t%s\n", lease.ID, lease.Status); err != nil {
		return err
	}
	if lease.BillingRecordID != "" {
		_, err := fmt.Fprintf(w, "billing_record_id\t%s\n", lease.BillingRecordID)
		return err
	}
	return nil
}

func pollLease(r *run, id string, every, timeout time.Duration) (api.Lease, []byte, error) {
	if every <= 0 {
		every = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last api.Lease
	var raw []byte
	for {
		lease, body, err := r.client.GetLease(id)
		if err != nil {
			return last, raw, err
		}
		last, raw = lease, body
		fmt.Fprintf(os.Stderr, "status\t%s\n", lease.Status)
		if api.IsWaitTerminal(lease.Status) {
			if api.IsWaitFailure(lease.Status) {
				return lease, raw, fmt.Errorf("lease %s %s", id, lease.Status)
			}
			return lease, raw, nil
		}
		if timeout > 0 && time.Now().After(deadline) {
			return lease, raw, fmt.Errorf("timed out waiting for lease %s (last status %s)", id, lease.Status)
		}
		time.Sleep(every)
	}
}
