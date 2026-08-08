package cmd

import (
	"fmt"

	pluginjobs "github.com/libops/sitectl-isle/pkg/jobs"
	"github.com/spf13/cobra"
)

var (
	recoveryOutput string
	recoveryInput  string
	recoveryYolo   bool
)

var recoveryCmd = &cobra.Command{
	Use:   "recovery",
	Short: "Back up, validate, and restore authoritative ISLE state",
	Long: `Manage ISLE recovery bundles.

Bundles contain the Drupal database and public/private files plus the Fcrepo database and
object data when Fcrepo is enabled. Secrets are deliberately excluded and must be recovered
from the organization's Vault backup. Solr, Blazegraph, broker queues, caches, and derivatives
are rebuildable state and are not included.`,
}

var recoveryPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show the full-state recovery contract",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), `Authoritative (backed up):
- Drupal database
- Drupal public and private files
- Fcrepo database and object data when enabled

Rebuildable (not backed up):
- Solr and Blazegraph indexes
- ActiveMQ queues
- IIIF caches and generated derivatives

External dependency:
- Recreate the target from the matching site Git revision and template provenance lock.
- Restore customer secrets from the organization's Vault backup.

Recovery gate:
- Validate the bundle, restore into a disposable context, run sitectl healthcheck,
  run sitectl verify --strict, and confirm customer-specific RPO/RTO before promotion.
`)
		return err
	},
}

var recoveryBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a checksummed full-state recovery bundle",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, err := commandSDK.GetContext()
		if err != nil {
			return err
		}
		return pluginjobs.RunRecoveryBackup(cmd, ctx, recoveryOutput)
	},
}

var recoveryValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a recovery bundle and display its state contract",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, err := commandSDK.GetContext()
		if err != nil {
			return err
		}
		manifest, err := pluginjobs.ValidateRecoveryBundle(ctx, recoveryInput)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), pluginjobs.FormatRecoveryManifest(manifest))
		return err
	},
}

var recoveryRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Replace authoritative ISLE state from a validated recovery bundle",
	Long: `Validate and restore an ISLE recovery bundle.

This is destructive. The command briefly enters Drupal maintenance mode and pauses mutation
consumers. After restoration, rebuild derived indexes and queues and run strict verification.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, err := commandSDK.GetContext()
		if err != nil {
			return err
		}
		return pluginjobs.RunRecoveryRestore(cmd, ctx, recoveryInput, recoveryYolo)
	},
}

func init() {
	recoveryBackupCmd.Flags().StringVar(&recoveryOutput, "output", "", "Absolute recovery bundle path on the context host")
	recoveryValidateCmd.Flags().StringVar(&recoveryInput, "input", "", "Absolute recovery bundle path on the context host")
	recoveryRestoreCmd.Flags().StringVar(&recoveryInput, "input", "", "Absolute recovery bundle path on the context host")
	recoveryRestoreCmd.Flags().BoolVar(&recoveryYolo, "yolo", false, "Skip confirmation before destructive authoritative-state replacement")
	must(recoveryBackupCmd.MarkFlagRequired("output"))
	must(recoveryValidateCmd.MarkFlagRequired("input"))
	must(recoveryRestoreCmd.MarkFlagRequired("input"))
	recoveryCmd.AddCommand(recoveryPlanCmd, recoveryBackupCmd, recoveryValidateCmd, recoveryRestoreCmd)
}
