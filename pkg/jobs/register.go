package jobs

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/job"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	sdk *plugin.SDK
)

func Register(s *plugin.SDK) {
	sdk = s
	sdk.RegisterContextJob(job.Spec{
		Name:        "fcrepo-db-backup",
		Description: "Export an Fcrepo SQL dump compressed as gzip",
	}, &fcrepoDBBackupJob{})
	sdk.RegisterContextJob(job.Spec{
		Name:        "fcrepo-db-import",
		Description: "Import an Fcrepo SQL dump compressed as gzip",
	}, &fcrepoDBImportJob{})
	sdk.RegisterContextJob(job.Spec{
		Name:        "recovery-backup",
		Description: "Create a checksummed full-state ISLE recovery bundle",
	}, &recoveryBackupJob{})
	sdk.RegisterContextJob(job.Spec{
		Name:        "recovery-restore",
		Description: "Restore an ISLE full-state recovery bundle",
	}, &recoveryRestoreJob{})
}

type recoveryBackupJob struct{ Output string }

func (j *recoveryBackupJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Output, "output", "", "Absolute recovery bundle path on the context host")
}

func (j *recoveryBackupJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Output) == "" {
		return fmt.Errorf("--output is required")
	}
	return RunRecoveryBackup(cmd, ctx, j.Output)
}

type recoveryRestoreJob struct {
	Input string
	Yolo  bool
}

func (j *recoveryRestoreJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Input, "input", "", "Absolute recovery bundle path on the context host")
	cmd.Flags().BoolVar(&j.Yolo, "yolo", false, "Skip confirmation before destructive authoritative-state replacement")
}

func (j *recoveryRestoreJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Input) == "" {
		return fmt.Errorf("--input is required")
	}
	return RunRecoveryRestore(cmd, ctx, j.Input, j.Yolo)
}

type fcrepoDBBackupJob struct {
	Output string
}

func (j *fcrepoDBBackupJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Output, "output", "", "Absolute output path on the host for the context this job runs on")
}

func (j *fcrepoDBBackupJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Output) == "" {
		return fmt.Errorf("--output is required")
	}
	return RunFcrepoDBBackup(cmd, ctx, j.Output)
}

type fcrepoDBImportJob struct {
	Input string
	Yolo  bool
}

func (j *fcrepoDBImportJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Input, "input", "", "Absolute input path on the host for the context this job runs on")
	cmd.Flags().BoolVar(&j.Yolo, "yolo", false, "Apply destructive database changes without confirmation")
}

func (j *fcrepoDBImportJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	if strings.TrimSpace(j.Input) == "" {
		return fmt.Errorf("--input is required")
	}
	return RunFcrepoDBImport(cmd, ctx, j.Input, j.Yolo)
}

func RunFcrepoDBBackup(cmd *cobra.Command, ctx *config.Context, outputPath string) error {
	if err := job.EnsurePathAbsentOnContext(ctx, outputPath); err != nil {
		return err
	}
	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerName, err := cli.GetContainerNameContext(cmd.Context(), ctx, "fcrepo")
	if err != nil {
		return err
	}
	if strings.TrimSpace(containerName) == "" {
		return fmt.Errorf("unable to find fcrepo container for context %q", ctx.Name)
	}

	password, err := ctx.ReadSmallFile(filepath.Join(ctx.ProjectDir, "secrets", "FCREPO_DB_PASSWORD"))
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "sitectl-fcrepo-db-backup-*.sql.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()

	gzipWriter := gzip.NewWriter(tempFile)
	defer gzipWriter.Close()

	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          []string{"mysqldump", "-h", "mariadb", "-u", "fcrepo", "fcrepo"},
		Env:          []string{"MYSQL_PWD=" + strings.TrimSpace(password)},
		AttachStdout: true,
		AttachStderr: true,
		Stdout:       gzipWriter,
		Stderr:       cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("fcrepo mysqldump failed with exit code %d", exitCode)
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return ctx.UploadFile(tempPath, outputPath)
}

func RunFcrepoDBImport(cmd *cobra.Command, ctx *config.Context, inputPath string, yolo bool) error {
	ok, err := job.ConfirmDatabaseReplacement(ctx.Name, "Fcrepo", inputPath, yolo)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("database import cancelled")
	}

	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	containerName, err := cli.GetContainerNameContext(cmd.Context(), ctx, "fcrepo")
	if err != nil {
		return err
	}
	if strings.TrimSpace(containerName) == "" {
		return fmt.Errorf("unable to find fcrepo container for context %q", ctx.Name)
	}

	password, err := ctx.ReadSmallFile(filepath.Join(ctx.ProjectDir, "secrets", "FCREPO_DB_PASSWORD"))
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp("", "sitectl-fcrepo-db-import-*.sql.gz")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err := job.DownloadContextFile(ctx, inputPath, tempPath); err != nil {
		return err
	}

	inputFile, err := os.Open(tempPath) // #nosec G304 -- tempPath is created by this process and populated before import.
	if err != nil {
		return err
	}
	defer inputFile.Close()

	gzipReader, err := gzip.NewReader(inputFile)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	exitCode, err := cli.Exec(cmd.Context(), docker.ExecOptions{
		Container:    containerName,
		Cmd:          []string{"mysql", "-h", "mariadb", "-u", "fcrepo", "fcrepo"},
		Env:          []string{"MYSQL_PWD=" + strings.TrimSpace(password)},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Stdin:        gzipReader,
		Stdout:       io.Discard,
		Stderr:       cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("fcrepo mysql import failed with exit code %d", exitCode)
	}
	return nil
}
