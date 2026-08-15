package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/libops/sitectl-isle/pkg/workbench"
	"github.com/libops/sitectl/pkg/config"
	corejob "github.com/libops/sitectl/pkg/job"
	"github.com/spf13/cobra"
)

// DrupalEndpointResolver resolves the selected context's canonical public
// Drupal URL through the plugin's ingress endpoint contract.
type DrupalEndpointResolver func(cmd *cobra.Command, ctx *config.Context) (string, error)

type workbenchRunner func(cmd *cobra.Command, ctx *config.Context, workbenchPath, configPath, inputCSV, logPath string) error

type workbenchRollbackConfirmer func(contextName, rollbackPath string, nodeCount int, yolo bool) (bool, error)

const (
	maxWorkbenchCSVBytes = int64(64 << 20)
	maxWorkbenchLogBytes = int64(256 << 20)
	maxWorkbenchConfig   = int64(4 << 20)
)

type workbenchPreflightJob struct {
	Input        string
	StagingRoot  string
	AllowedRoots []string
	Columns      []string
}

func (j *workbenchPreflightJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.Input, "input", "", "Absolute Workbench CSV path on the context host")
	cmd.Flags().StringVar(&j.StagingRoot, "staging-root", "/mnt/islandora_staging", "Root applied to relative media paths")
	cmd.Flags().StringSliceVar(&j.AllowedRoots, "allowed-root", nil, "Allowed absolute media root (repeatable; defaults to staging-root, /home, and /mnt)")
	cmd.Flags().StringSliceVar(&j.Columns, "column", workbench.DefaultMediaColumns(), "CSV file-bearing column (repeatable)")
}

func (j *workbenchPreflightJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	input, err := exactContextPath("--input", j.Input)
	if err != nil {
		return err
	}
	runCtx := commandContext(cmd)
	inputData, err := downloadContextBytes(runCtx, ctx, input, maxWorkbenchCSVBytes)
	if err != nil {
		return err
	}
	references, err := workbench.ParseMediaReferences(bytes.NewReader(inputData), j.Columns)
	if err != nil {
		return err
	}
	roots := j.AllowedRoots
	if !cmd.Flags().Changed("allowed-root") {
		roots = []string{j.StagingRoot, "/home", "/mnt"}
	}
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return fmt.Errorf("create context file accessor: %w", err)
	}
	defer accessor.Close()
	inspections, err := workbench.InspectMediaReferences(references, j.StagingRoot, roots, &contextPathInspector{
		accessor: accessor,
		ctx:      ctx,
		runCtx:   runCtx,
	})
	if err != nil {
		return err
	}

	counts := map[workbench.PathStatus]int{}
	for _, inspection := range inspections {
		counts[inspection.Status]++
		if inspection.Status == workbench.PathAvailable {
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\trow=%d\tcolumn=%s\tpath=%s\treason=%v\n",
			inspection.Status,
			inspection.Reference.Row,
			inspection.Reference.Column,
			inspection.ResolvedPath,
			inspection.Err,
		)
	}
	if counts[workbench.PathMissing]+counts[workbench.PathUnreadable]+counts[workbench.PathInvalid] > 0 {
		return fmt.Errorf("workbench media preflight failed: available=%d missing=%d unreadable=%d invalid=%d",
			counts[workbench.PathAvailable], counts[workbench.PathMissing], counts[workbench.PathUnreadable], counts[workbench.PathInvalid])
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workbench media preflight passed: %d referenced files are readable\n", counts[workbench.PathAvailable])
	return nil
}

type contextPathInspector struct {
	accessor *config.FileAccessor
	ctx      *config.Context
	runCtx   context.Context
}

func (i *contextPathInspector) Lstat(name string) (fs.FileInfo, error) {
	return i.accessor.Lstat(name)
}

func (i *contextPathInspector) Readable(name string) error {
	_, err := i.ctx.RunQuietCommandContext(i.runCtx, exec.Command("test", "-r", name)) // #nosec G204 -- test is fixed and receives one exact context path without a shell.
	return err
}

type workbenchReconcileSupplementalJob struct {
	InputDir    string
	RollbackCSV string
	Contract    string
}

func (j *workbenchReconcileSupplementalJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.InputDir, "input-dir", "", "Absolute directory containing Crosswalk Workbench artifacts on the context host")
	cmd.Flags().StringVar(&j.RollbackCSV, "rollback-csv", "", "Exact absolute Workbench rollback CSV path; required only to resolve blank node IDs")
	cmd.Flags().StringVar(&j.Contract, "contract", "", "Absolute trusted Crosswalk Workbench contract path provisioned outside input-dir")
}

func (j *workbenchReconcileSupplementalJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	inputDir, err := exactContextPath("--input-dir", j.InputDir)
	if err != nil {
		return err
	}
	contractPath, err := exactContextPath("--contract", j.Contract)
	if err != nil {
		return err
	}
	if pathWithinDirectory(contractPath, inputDir) {
		return fmt.Errorf("--contract must be trusted site configuration outside --input-dir; %q is inside the uploaded batch", contractPath)
	}
	if err := inspectContextDirectory(ctx, inputDir); err != nil {
		return fmt.Errorf("validate Crosswalk artifact directory: %w", err)
	}
	realInputDir, err := contextRealPath(ctx, inputDir)
	if err != nil {
		return fmt.Errorf("resolve Crosswalk artifact directory %q: %w", inputDir, err)
	}
	realContractPath, err := contextRealPath(ctx, contractPath)
	if err != nil {
		return fmt.Errorf("resolve trusted Crosswalk artifact contract %q: %w", contractPath, err)
	}
	if pathWithinDirectory(realContractPath, realInputDir) {
		return fmt.Errorf("--contract must resolve outside --input-dir; %q resolves inside the uploaded batch", contractPath)
	}
	outputPath := path.Join(inputDir, "target.add_media.csv")
	runCtx := commandContext(cmd)
	contract, err := loadCrosswalkArtifactContract(runCtx, ctx, contractPath)
	if err != nil {
		return err
	}
	manifest, err := loadCrosswalkArtifactManifest(runCtx, ctx, inputDir)
	if err != nil {
		return err
	}
	if err := contract.ValidateManifest(manifest); err != nil {
		return fmt.Errorf("refusing untrusted Crosswalk artifact batch: %w", err)
	}
	if _, pending := manifest.Artifact("target.pending_supplemental.csv"); !pending {
		if _, unpublished := manifest.Artifact("target.unpublished_supplemental.csv"); !unpublished {
			return fmt.Errorf("%s lists neither supplemental artifact required by this job", workbench.CrosswalkArtifactManifestName)
		}
	}
	if manifest.Policy == nil {
		return fmt.Errorf("%s has no supplemental reconciliation policy", workbench.CrosswalkArtifactManifestName)
	}
	if err := rejectUnmanifestedCrosswalkArtifacts(ctx, inputDir, manifest); err != nil {
		return err
	}
	artifactData, err := loadCrosswalkArtifactSet(runCtx, ctx, inputDir, manifest)
	if err != nil {
		return err
	}

	var createReader, rollbackReader io.Reader
	if strings.TrimSpace(j.RollbackCSV) != "" {
		rollbackPath, pathErr := exactContextPath("--rollback-csv", j.RollbackCSV)
		if pathErr != nil {
			return pathErr
		}
		createData, listed := artifactData["target.csv"]
		if !listed {
			return fmt.Errorf("crosswalk positional reconciliation requires target.csv to be listed in %s", workbench.CrosswalkArtifactManifestName)
		}
		rollbackData, readErr := downloadContextBytes(runCtx, ctx, rollbackPath, maxWorkbenchCSVBytes)
		if readErr != nil {
			return readErr
		}
		createReader = bytes.NewReader(createData)
		rollbackReader = bytes.NewReader(rollbackData)
	}

	var existingReader *bytes.Reader
	if existingData, listed := artifactData["target.add_media.csv"]; listed {
		existingReader = bytes.NewReader(existingData)
	} else if exists, existsErr := contextPathExists(ctx, outputPath); existsErr != nil {
		return existsErr
	} else if exists {
		return fmt.Errorf("refusing unmanifested Crosswalk artifact %q", outputPath)
	}

	artifactSpecs := []struct {
		filename           string
		defaultMediaUseTID string
		defaultPublished   string
	}{
		{
			filename:           "target.pending_supplemental.csv",
			defaultMediaUseTID: manifest.Policy.SupplementalMediaUseTID,
			defaultPublished:   manifest.Policy.PendingSupplementalPublished,
		},
		{
			filename:           "target.unpublished_supplemental.csv",
			defaultMediaUseTID: manifest.Policy.UnpublishedSupplementalMediaUseTID,
			defaultPublished:   manifest.Policy.UnpublishedSupplementalPublished,
		},
	}
	artifacts := make([]workbench.SupplementalArtifact, 0, len(artifactSpecs))
	for _, spec := range artifactSpecs {
		data, listed := artifactData[spec.filename]
		if !listed {
			continue
		}
		artifacts = append(artifacts, workbench.SupplementalArtifact{
			Name:               spec.filename,
			Reader:             bytes.NewReader(data),
			DefaultMediaUseTID: spec.defaultMediaUseTID,
			DefaultPublished:   spec.defaultPublished,
		})
	}
	if len(artifacts) == 0 {
		return fmt.Errorf("neither pending supplemental artifact exists in %q", inputDir)
	}

	var existingReaderValue io.Reader
	if existingReader != nil {
		existingReaderValue = existingReader
	}
	rows, err := workbench.ReconcileSupplementalMedia(createReader, rollbackReader, existingReaderValue, artifacts)
	if err != nil {
		return err
	}
	var output bytes.Buffer
	if err := workbench.WriteAddMediaCSV(&output, rows); err != nil {
		return err
	}
	if err := writeContextArtifact(ctx, outputPath, output.Bytes()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Reconciled %d deduplicated media rows into %s using trusted Crosswalk spec %s@%s (%s)\n",
		len(rows), outputPath, manifest.Spec.Name, manifest.Spec.Version, manifest.Spec.Fingerprint)
	return nil
}

var crosswalkArtifactFilenames = []string{
	"target.csv",
	"target.update.csv",
	"target.add_media.csv",
	"agents.csv",
	"target.pending_supplemental.csv",
	"target.unpublished_supplemental.csv",
}

func loadCrosswalkArtifactContract(runCtx context.Context, ctx *config.Context, contractPath string) (workbench.ArtifactContract, error) {
	contractData, err := downloadContextBytes(runCtx, ctx, contractPath, maxWorkbenchConfig)
	if err != nil {
		return workbench.ArtifactContract{}, fmt.Errorf("read trusted Crosswalk artifact contract %q: %w", contractPath, err)
	}
	contract, err := workbench.ParseArtifactContract(bytes.NewReader(contractData))
	if err != nil {
		return workbench.ArtifactContract{}, fmt.Errorf("validate trusted Crosswalk artifact contract %q: %w", contractPath, err)
	}
	return contract, nil
}

func loadCrosswalkArtifactManifest(runCtx context.Context, ctx *config.Context, inputDir string) (workbench.ArtifactManifest, error) {
	manifestPath := path.Join(inputDir, workbench.CrosswalkArtifactManifestName)
	manifestData, err := downloadContextBytes(runCtx, ctx, manifestPath, maxWorkbenchConfig)
	if err != nil {
		return workbench.ArtifactManifest{}, fmt.Errorf("crosswalk workflow requires valid %s in %q: %w", workbench.CrosswalkArtifactManifestName, inputDir, err)
	}
	manifest, err := workbench.ParseArtifactManifest(bytes.NewReader(manifestData))
	if err != nil {
		return workbench.ArtifactManifest{}, fmt.Errorf("validate %q: %w", manifestPath, err)
	}
	return manifest, nil
}

func loadCrosswalkArtifactSet(runCtx context.Context, ctx *config.Context, inputDir string, manifest workbench.ArtifactManifest) (map[string][]byte, error) {
	artifacts := make(map[string][]byte, len(manifest.Artifacts))
	for _, descriptor := range manifest.Artifacts {
		artifactPath := path.Join(inputDir, descriptor.Path)
		data, readErr := downloadContextBytes(runCtx, ctx, artifactPath, maxWorkbenchCSVBytes)
		if readErr != nil {
			return nil, fmt.Errorf("read manifested Crosswalk artifact %q: %w", descriptor.Path, readErr)
		}
		if validateErr := manifest.ValidateArtifact(descriptor.Path, data); validateErr != nil {
			return nil, validateErr
		}
		artifacts[descriptor.Path] = data
	}
	return artifacts, nil
}

func pathWithinDirectory(filename, directory string) bool {
	return filename == directory || strings.HasPrefix(filename, directory+"/")
}

func contextRealPath(ctx *config.Context, filename string) (string, error) {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return "", fmt.Errorf("create context file accessor: %w", err)
	}
	defer func() { _ = accessor.Close() }()
	resolved, err := accessor.RealPath(filename)
	if err != nil {
		return "", err
	}
	return path.Clean(resolved), nil
}

func rejectUnmanifestedCrosswalkArtifacts(ctx *config.Context, inputDir string, manifest workbench.ArtifactManifest) error {
	for _, filename := range crosswalkArtifactFilenames {
		if _, listed := manifest.Artifact(filename); listed {
			continue
		}
		artifactPath := path.Join(inputDir, filename)
		exists, err := contextPathExists(ctx, artifactPath)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("refusing unmanifested Crosswalk artifact %q", artifactPath)
		}
	}
	return nil
}

type workbenchRetryMediaJob struct {
	FailedLog     string
	Output        string
	Config        string
	Workbench     string
	RetryLog      string
	resolveDrupal DrupalEndpointResolver
	run           workbenchRunner
}

func (j *workbenchRetryMediaJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.FailedLog, "failed-log", "", "Exact absolute failed Workbench log path on the context host")
	cmd.Flags().StringVar(&j.Output, "output", "", "Absolute retry add_media CSV path on the context host")
	cmd.Flags().StringVar(&j.Config, "config", "", "Absolute Workbench add_media configuration path on the context host")
	cmd.Flags().StringVar(&j.Workbench, "workbench", "", "Absolute Workbench script path on the context host")
	cmd.Flags().StringVar(&j.RetryLog, "retry-log", "", "Absolute log path for the retry on the context host")
}

func (j *workbenchRetryMediaJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	failedLog, err := exactContextPath("--failed-log", j.FailedLog)
	if err != nil {
		return err
	}
	output, err := exactContextPath("--output", j.Output)
	if err != nil {
		return err
	}
	configPath, err := exactContextPath("--config", j.Config)
	if err != nil {
		return err
	}
	workbenchPath, err := exactContextPath("--workbench", j.Workbench)
	if err != nil {
		return err
	}
	retryLog, err := exactContextPath("--retry-log", j.RetryLog)
	if err != nil {
		return err
	}
	if err := ensureDistinctPaths(map[string]string{"failed log": failedLog, "retry CSV": output, "retry log": retryLog}); err != nil {
		return err
	}
	runCtx := commandContext(cmd)
	failedData, err := downloadContextBytes(runCtx, ctx, failedLog, maxWorkbenchLogBytes)
	if err != nil {
		return err
	}
	rows, err := workbench.PlanMediaRetry(bytes.NewReader(failedData))
	if err != nil {
		return err
	}
	configData, err := readSmallContextArtifact(ctx, configPath, maxWorkbenchConfig)
	if err != nil {
		return err
	}
	guardedConfigData, validatedConfig, err := validateWorkbenchExecutionConfig(cmd, ctx, j.resolveDrupal, configPath, configData, "add_media", retryLog)
	if err != nil {
		return err
	}
	if err := requireReadableContextFile(runCtx, ctx, workbenchPath); err != nil {
		return fmt.Errorf("validate Workbench script: %w", err)
	}
	if err := ensureContextPathAbsent(ctx, output); err != nil {
		return err
	}
	if err := ensureContextPathAbsent(ctx, retryLog); err != nil {
		return err
	}

	var retryCSV bytes.Buffer
	if err := workbench.WriteMediaRetryCSV(&retryCSV, rows); err != nil {
		return err
	}
	if err := uploadNewContextArtifact(runCtx, ctx, output, retryCSV.Bytes()); err != nil {
		return err
	}
	configSnapshot, removeConfigSnapshot, err := snapshotContextArtifact(runCtx, ctx, configPath, ".yml", guardedConfigData)
	if err != nil {
		return err
	}
	defer func() { _ = removeConfigSnapshot() }()
	csvSnapshot, removeCSVSnapshot, err := snapshotContextArtifact(runCtx, ctx, output, ".csv", retryCSV.Bytes())
	if err != nil {
		return err
	}
	defer func() { _ = removeCSVSnapshot() }()
	runner := j.run
	if runner == nil {
		runner = runWorkbench
	}
	if err := runner(cmd, ctx, workbenchPath, configSnapshot, csvSnapshot, retryLog); err != nil {
		return fmt.Errorf("workbench media retry failed: %w", err)
	}
	retryLogData, err := downloadContextBytes(runCtx, ctx, retryLog, maxWorkbenchLogBytes)
	if err != nil {
		return err
	}
	if err := workbench.ValidateMediaRetryLog(bytes.NewReader(retryLogData), rows, validatedConfig.Host); err != nil {
		return fmt.Errorf("workbench media retry did not complete cleanly: %w", err)
	}
	reportSnapshotCleanup(cmd, removeCSVSnapshot())
	reportSnapshotCleanup(cmd, removeConfigSnapshot())
	fmt.Fprintf(cmd.OutOrStdout(), "Workbench media retry succeeded for %d deduplicated rows\n", len(rows))
	return nil
}

type workbenchRollbackJob struct {
	RollbackCSV   string
	Config        string
	Workbench     string
	Log           string
	Yolo          bool
	resolveDrupal DrupalEndpointResolver
	confirm       workbenchRollbackConfirmer
	run           workbenchRunner
}

func (j *workbenchRollbackJob) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&j.RollbackCSV, "rollback-csv", "", "Exact absolute Workbench rollback CSV path on the context host")
	cmd.Flags().StringVar(&j.Config, "config", "", "Absolute Workbench delete configuration path on the context host")
	cmd.Flags().StringVar(&j.Workbench, "workbench", "", "Absolute Workbench script path on the context host")
	cmd.Flags().StringVar(&j.Log, "log", "", "Absolute rollback log path on the context host")
	cmd.Flags().BoolVar(&j.Yolo, "yolo", false, "Delete rollback nodes without interactive confirmation")
}

func (j *workbenchRollbackJob) Run(cmd *cobra.Command, ctx *config.Context) error {
	rollbackPath, err := exactContextPath("--rollback-csv", j.RollbackCSV)
	if err != nil {
		return err
	}
	configPath, err := exactContextPath("--config", j.Config)
	if err != nil {
		return err
	}
	workbenchPath, err := exactContextPath("--workbench", j.Workbench)
	if err != nil {
		return err
	}
	logPath, err := exactContextPath("--log", j.Log)
	if err != nil {
		return err
	}
	if err := ensureDistinctPaths(map[string]string{"rollback CSV": rollbackPath, "rollback log": logPath}); err != nil {
		return err
	}
	runCtx := commandContext(cmd)
	rollbackData, err := downloadContextBytes(runCtx, ctx, rollbackPath, maxWorkbenchCSVBytes)
	if err != nil {
		return err
	}
	nodeIDs, err := workbench.ParseRollbackCSV(bytes.NewReader(rollbackData))
	if err != nil {
		return err
	}
	configData, err := readSmallContextArtifact(ctx, configPath, maxWorkbenchConfig)
	if err != nil {
		return err
	}
	guardedConfigData, validatedConfig, err := validateWorkbenchExecutionConfig(cmd, ctx, j.resolveDrupal, configPath, configData, "delete", logPath)
	if err != nil {
		return err
	}
	if err := requireReadableContextFile(runCtx, ctx, workbenchPath); err != nil {
		return fmt.Errorf("validate Workbench script: %w", err)
	}
	if err := ensureContextPathAbsent(ctx, logPath); err != nil {
		return err
	}
	confirm := j.confirm
	if confirm == nil {
		confirm = confirmWorkbenchRollback
	}
	confirmed, err := confirm(ctx.Name, rollbackPath, len(nodeIDs), j.Yolo)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("workbench rollback cancelled")
	}
	configSnapshot, removeConfigSnapshot, err := snapshotContextArtifact(runCtx, ctx, configPath, ".yml", guardedConfigData)
	if err != nil {
		return err
	}
	defer func() { _ = removeConfigSnapshot() }()
	rollbackSnapshot, removeRollbackSnapshot, err := snapshotContextArtifact(runCtx, ctx, rollbackPath, ".csv", rollbackData)
	if err != nil {
		return err
	}
	defer func() { _ = removeRollbackSnapshot() }()
	runner := j.run
	if runner == nil {
		runner = runWorkbench
	}
	if err := runner(cmd, ctx, workbenchPath, configSnapshot, rollbackSnapshot, logPath); err != nil {
		return fmt.Errorf("workbench rollback failed: %w", err)
	}
	rollbackLogData, err := downloadContextBytes(runCtx, ctx, logPath, maxWorkbenchLogBytes)
	if err != nil {
		return err
	}
	if err := workbench.ValidateRollbackLog(bytes.NewReader(rollbackLogData), nodeIDs, validatedConfig.Host); err != nil {
		return fmt.Errorf("workbench rollback did not complete cleanly: %w", err)
	}
	reportSnapshotCleanup(cmd, removeRollbackSnapshot())
	reportSnapshotCleanup(cmd, removeConfigSnapshot())
	fmt.Fprintf(cmd.OutOrStdout(), "Workbench rollback succeeded for %d nodes from %s\n", len(nodeIDs), rollbackPath)
	return nil
}

func reportSnapshotCleanup(cmd *cobra.Command, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Warning: Workbench operation succeeded, but snapshot cleanup failed: %v\n", err)
}

func runWorkbench(cmd *cobra.Command, ctx *config.Context, workbenchPath, configPath, inputCSV, logPath string) error {
	_, err := ctx.RunCommandContext(commandContext(cmd), exec.Command(
		"python3",
		workbenchPath,
		"--config", configPath,
		"--input_csv", inputCSV,
		"--log_file_path", logPath,
	)) // #nosec G204 -- executable and flags are fixed; exact context paths are passed without a shell.
	return err
}

func validateWorkbenchExecutionConfig(cmd *cobra.Command, ctx *config.Context, resolveDrupal DrupalEndpointResolver, configPath string, data []byte, expectedTask, logPath string) ([]byte, workbench.Config, error) {
	guardedData, cfg, err := workbench.GuardedConfigSnapshot(data, expectedTask, logPath)
	if err != nil {
		return nil, workbench.Config{}, fmt.Errorf("validate Workbench config %q: %w", configPath, err)
	}
	if resolveDrupal == nil {
		return nil, workbench.Config{}, fmt.Errorf("resolve Drupal endpoint for Workbench %s: ISLE endpoint provider is not configured", expectedTask)
	}
	resolvedURL, err := resolveDrupal(cmd, ctx)
	if err != nil {
		return nil, workbench.Config{}, fmt.Errorf("resolve Drupal endpoint for context %q before Workbench %s: %w", ctx.Name, expectedTask, err)
	}
	configuredOrigin, err := workbench.NormalizeSiteURL(cfg.Host)
	if err != nil {
		return nil, workbench.Config{}, fmt.Errorf("workbench config %q has invalid host: %w", configPath, err)
	}
	resolvedOrigin, err := workbench.NormalizeSiteURL(resolvedURL)
	if err != nil {
		return nil, workbench.Config{}, fmt.Errorf("sitectl resolved an invalid Drupal endpoint %q for context %q: %w", resolvedURL, ctx.Name, err)
	}
	if configuredOrigin != resolvedOrigin {
		return nil, workbench.Config{}, fmt.Errorf("refusing Workbench %s: config %q site URL %q does not match context %q Drupal URL %q; update the config host or select the intended --context", expectedTask, configPath, configuredOrigin, ctx.Name, resolvedOrigin)
	}
	return guardedData, cfg, nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd != nil && cmd.Context() != nil {
		return cmd.Context()
	}
	return context.Background()
}

func confirmWorkbenchRollback(contextName, rollbackPath string, nodeCount int, yolo bool) (bool, error) {
	if yolo {
		return true, nil
	}
	answer, err := config.GetInput(
		fmt.Sprintf("About to delete %d Islandora nodes and any attached media/files selected by Workbench from context %q.", nodeCount, contextName),
		fmt.Sprintf("Only node IDs in exact rollback artifact %q will be used.", rollbackPath),
		"Continue? [y/N]: ",
	)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func exactContextPath(flag, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", flag)
	}
	cleaned := path.Clean(value)
	if !path.IsAbs(cleaned) || cleaned == "/" {
		return "", fmt.Errorf("%s must be an absolute, non-root context-host path", flag)
	}
	if cleaned != value {
		return "", fmt.Errorf("%s must be a canonical context-host path; use %q", flag, cleaned)
	}
	return cleaned, nil
}

func ensureDistinctPaths(named map[string]string) error {
	seen := make(map[string]string, len(named))
	for name, value := range named {
		if existing, ok := seen[value]; ok {
			return fmt.Errorf("%s and %s must use different paths, both are %q", existing, name, value)
		}
		seen[value] = name
	}
	return nil
}

// Context-host metadata checks are advisory guards against operator mistakes.
// Remote SFTP cannot hold a path stable across a later operation, so Workbench
// jobs assume these operator-owned artifact paths are not mutated concurrently.
func contextPathExists(ctx *config.Context, filename string) (bool, error) {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return false, fmt.Errorf("create context file accessor: %w", err)
	}
	defer accessor.Close()
	_, err = accessor.Lstat(filename)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect context path %q: %w", filename, err)
}

func ensureContextPathAbsent(ctx *config.Context, filename string) error {
	exists, err := contextPathExists(ctx, filename)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("refusing to overwrite existing context-host artifact %q", filename)
	}
	return nil
}

func inspectContextRegularFile(ctx *config.Context, filename string, maxBytes int64) error {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return fmt.Errorf("create context file accessor: %w", err)
	}
	defer accessor.Close()
	info, err := accessor.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect context artifact %q: %w", filename, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing context artifact symlink %q", filename)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("context artifact %q is not a regular file", filename)
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("context artifact %q is %d bytes, exceeds %d-byte limit", filename, info.Size(), maxBytes)
	}
	return nil
}

func inspectContextDirectory(ctx *config.Context, directory string) error {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return fmt.Errorf("create context file accessor: %w", err)
	}
	defer accessor.Close()
	info, err := accessor.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect context directory %q: %w", directory, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("refusing context directory symlink %q", directory)
	}
	if !info.IsDir() {
		return fmt.Errorf("context path %q is not a directory", directory)
	}
	return nil
}

func downloadContextBytes(runCtx context.Context, ctx *config.Context, filename string, maxBytes int64) ([]byte, error) {
	if err := inspectContextRegularFile(ctx, filename, maxBytes); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "sitectl-workbench-artifact-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	tempPath := path.Join(tempDir, "artifact")
	if err := corejob.DownloadContextFileContext(runCtx, ctx, filename, tempPath); err != nil {
		return nil, fmt.Errorf("download context artifact %q: %w", filename, err)
	}
	data, err := os.ReadFile(tempPath) // #nosec G304 -- path is a private file created above.
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("context artifact %q changed while reading and exceeds %d-byte limit", filename, maxBytes)
	}
	return data, nil
}

func readSmallContextArtifact(ctx *config.Context, filename string, maxBytes int64) ([]byte, error) {
	if err := inspectContextRegularFile(ctx, filename, maxBytes); err != nil {
		return nil, err
	}
	data, err := ctx.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("context artifact %q changed while reading and exceeds %d-byte limit", filename, maxBytes)
	}
	return data, nil
}

func uploadNewContextArtifact(runCtx context.Context, ctx *config.Context, destination string, data []byte) error {
	tempFile, err := os.CreateTemp("", "sitectl-workbench-output-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return corejob.UploadContextFile(runCtx, ctx, tempPath, destination)
}

func snapshotContextArtifact(runCtx context.Context, ctx *config.Context, sourcePath, suffix string, data []byte) (string, func() error, error) {
	parent := path.Dir(sourcePath)
	for attempts := 0; attempts < 8; attempts++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return "", nil, err
		}
		destination := path.Join(parent, ".sitectl-workbench-snapshot-"+hex.EncodeToString(random)+suffix)
		if err := uploadNewContextArtifact(runCtx, ctx, destination, data); err != nil {
			if strings.Contains(err.Error(), "file exists") || strings.Contains(err.Error(), "overwrite existing") {
				continue
			}
			return "", nil, fmt.Errorf("create immutable context snapshot for %q: %w", sourcePath, err)
		}
		removed := false
		remove := func() error {
			if removed {
				return nil
			}
			if err := ctx.RemoveFile(destination); err != nil {
				return fmt.Errorf("remove context snapshot %q: %w", destination, err)
			}
			removed = true
			return nil
		}
		return destination, remove, nil
	}
	return "", nil, fmt.Errorf("unable to allocate a unique context snapshot beside %q", sourcePath)
}

// The destination type check and atomic publish are separate on remote
// contexts. Workbench artifact paths are operator-controlled, so concurrent
// mutation between them is outside this workflow's trust model.
func writeContextArtifact(ctx *config.Context, destination string, data []byte) error {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return fmt.Errorf("create context file accessor: %w", err)
	}
	info, statErr := accessor.Lstat(destination)
	closeErr := accessor.Close()
	if statErr == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace context artifact symlink %q", destination)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular context artifact %q", destination)
		}
	}
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) && !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect context artifact destination %q: %w", destination, statErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return ctx.WriteFile(destination, data)
}

func requireReadableContextFile(runCtx context.Context, ctx *config.Context, filename string) error {
	accessor, err := ctx.NewFileAccessor()
	if err != nil {
		return err
	}
	info, err := accessor.Lstat(filename)
	if closeErr := accessor.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%q must be a non-symlink regular file", filename)
	}
	_, err = ctx.RunQuietCommandContext(runCtx, exec.Command("test", "-r", filename)) // #nosec G204 -- test is fixed and receives one exact context path without a shell.
	return err
}
