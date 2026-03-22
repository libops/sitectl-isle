package cmd

import (
	"fmt"
	"time"

	pluginjobs "github.com/libops/sitectl-isle/pkg/jobs"
	"github.com/libops/sitectl/pkg/config"
	corejob "github.com/libops/sitectl/pkg/job"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	syncSourceContext string
	syncTargetContext string
	syncFresh         bool
	syncBackupDir     string
	syncYolo          bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync ISLE artifacts between contexts",
}

var syncFcrepoCmd = &cobra.Command{
	Use:     "fcrepo",
	Aliases: []string{"database", "db"},
	Short:   "Sync the Fcrepo database from one context to another",
	RunE: func(cmd *cobra.Command, args []string) error {
		progress := plugin.NewProgressLine(cmd.ErrOrStderr(), "Syncing Fcrepo Database", "Resolving contexts")
		defer progress.Close()

		sourceCtx, targetCtx, err := corejob.ResolveContextPair(syncSourceContext, syncTargetContext)
		if err != nil {
			return err
		}

		workDir, cleanupWorkDir, err := corejob.MakeTempWorkDir("sitectl-isle-sync-fcrepo-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer cleanupWorkDir()

		progress.Report("Syncing Fcrepo Database", fmt.Sprintf("Resolving source artifact from %s", sourceCtx.Name))
		sourceArtifactPath, err := resolveSourceDBArtifact(cmd, sourceCtx)
		if err != nil {
			return err
		}

		progress.Report("Syncing Fcrepo Database", fmt.Sprintf("Staging artifact from %s to %s", sourceCtx.Name, targetCtx.Name))
		targetHostPath, cleanupTarget, err := corejob.StageArtifactBetweenContexts(
			cmd.Context(),
			sourceCtx,
			targetCtx,
			sourceArtifactPath,
			workDir,
			"fcrepo.sql.gz",
			"sitectl-isle-sync",
		)
		if err != nil {
			return fmt.Errorf("stage fcrepo database artifact from %q to %q: %w", sourceCtx.Name, targetCtx.Name, err)
		}
		defer cleanupTarget()

		progress.Report("Syncing Fcrepo Database", fmt.Sprintf("Importing into %s", targetCtx.Name))
		if !syncYolo {
			progress.Close()
		}
		if err := pluginjobs.RunFcrepoDBImport(cmd, targetCtx, targetHostPath, syncYolo); err != nil {
			return fmt.Errorf("import fcrepo database into %q: %w", targetCtx.Name, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Fcrepo database synced from %s to %s\n", sourceCtx.Name, targetCtx.Name)
		return nil
	},
}

func init() {
	syncFcrepoCmd.Flags().StringVar(&syncSourceContext, "source", "", "Source sitectl context")
	syncFcrepoCmd.Flags().StringVar(&syncTargetContext, "target", "", "Target sitectl context")
	syncFcrepoCmd.Flags().BoolVar(&syncFresh, "fresh", false, "Always run a fresh source Fcrepo database backup instead of reusing today/yesterday if available")
	syncFcrepoCmd.Flags().StringVar(&syncBackupDir, "backup-dir", "/tmp/sitectl-isle-jobs/fcrepo-db-backup", "Source host directory used to cache Fcrepo database backup artifacts for sync")
	syncFcrepoCmd.Flags().BoolVar(&syncYolo, "yolo", false, "Apply destructive database changes without confirmation")
	must(syncFcrepoCmd.MarkFlagRequired("source"))
	must(syncFcrepoCmd.MarkFlagRequired("target"))

	syncCmd.AddCommand(syncFcrepoCmd)
}

func resolveSourceDBArtifact(cmd *cobra.Command, ctx *config.Context) (string, error) {
	return corejob.ResolveRecentArtifact(ctx, syncBackupDir, "fcrepo.sql.gz", syncFresh, time.Now().UTC(), func(path string) error {
		if err := pluginjobs.RunFcrepoDBBackup(cmd, ctx, path); err != nil {
			return fmt.Errorf("run source fcrepo-db-backup job on %q: %w", ctx.Name, err)
		}
		return nil
	})
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
