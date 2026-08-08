package jobs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	corejob "github.com/libops/sitectl/pkg/job"
	"github.com/spf13/cobra"
)

const (
	recoveryFormatVersion = 1
	manifestName          = "manifest.json"
	drupalDatabaseName    = "drupal.sql.gz"
	drupalFilesName       = "drupal-files.tar.gz"
	fcrepoDatabaseName    = "fcrepo.sql.gz"
	fcrepoDataName        = "fcrepo-data.tar.gz"
)

var recoveryAllowedFiles = map[string]bool{
	manifestName:       true,
	drupalDatabaseName: true,
	drupalFilesName:    true,
	fcrepoDatabaseName: true,
	fcrepoDataName:     true,
}

// RecoveryManifest is the stable, inspectable contract embedded in every bundle.
type RecoveryManifest struct {
	FormatVersion    int               `json:"format_version"`
	CreatedAt        time.Time         `json:"created_at"`
	FcrepoEnabled    bool              `json:"fcrepo_enabled"`
	Artifacts        map[string]string `json:"artifacts"`
	Authoritative    []string          `json:"authoritative_state"`
	Rebuildable      []string          `json:"rebuildable_state"`
	RequiredExternal []string          `json:"required_external_state"`
	ExcludedSecrets  string            `json:"excluded_secrets"`
}

// RunRecoveryBackup creates a portable backup of every authoritative ISLE data store.
func RunRecoveryBackup(cmd *cobra.Command, ctx *config.Context, outputPath string) (retErr error) {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	if err := corejob.EnsurePathAbsentOnContext(ctx, outputPath); err != nil {
		return err
	}
	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	containers, err := recoveryContainers(cmd.Context(), cli, ctx)
	if err != nil {
		return err
	}
	resume, err := quiesceISLE(cmd, ctx, cli, containers)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, resume()) }()

	workDir, err := os.MkdirTemp("", "sitectl-isle-recovery-backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	if err := dumpDatabase(cmd.Context(), cli, containers.mariadb, "drupal_default", "drupal_default", ctx, "DRUPAL_DEFAULT_DB_PASSWORD", filepath.Join(workDir, drupalDatabaseName), cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("back up Drupal database: %w", err)
	}
	if err := archiveContainerPaths(cmd.Context(), cli, containers.drupal, "/var/www/drupal", []string{"web/sites/default/files", "private"}, filepath.Join(workDir, drupalFilesName), cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("back up Drupal files: %w", err)
	}

	manifest := RecoveryManifest{
		FormatVersion:    recoveryFormatVersion,
		CreatedAt:        time.Now().UTC(),
		Artifacts:        map[string]string{},
		Authoritative:    []string{"Drupal database", "Drupal public files", "Drupal private files"},
		Rebuildable:      []string{"Solr indexes", "ActiveMQ queues", "Blazegraph index", "IIIF caches", "generated derivatives"},
		RequiredExternal: []string{"site Git checkout and template provenance lock", "organization Vault backup"},
		ExcludedSecrets:  "Secrets are intentionally excluded; restore them from the organization's Vault backup before recovery.",
	}
	if containers.fcrepo != "" {
		manifest.FcrepoEnabled = true
		manifest.Authoritative = append(manifest.Authoritative, "Fcrepo database", "Fcrepo object data")
		if err := dumpDatabase(cmd.Context(), cli, containers.mariadb, "fcrepo", "fcrepo", ctx, "FCREPO_DB_PASSWORD", filepath.Join(workDir, fcrepoDatabaseName), cmd.ErrOrStderr()); err != nil {
			return fmt.Errorf("back up Fcrepo database: %w", err)
		}
		if err := archiveContainerPaths(cmd.Context(), cli, containers.fcrepo, "/", []string{"data"}, filepath.Join(workDir, fcrepoDataName), cmd.ErrOrStderr()); err != nil {
			return fmt.Errorf("back up Fcrepo data: %w", err)
		}
	}

	for _, name := range requiredRecoveryArtifacts(manifest.FcrepoEnabled) {
		digest, err := fileSHA256(filepath.Join(workDir, name))
		if err != nil {
			return err
		}
		manifest.Artifacts[name] = digest
	}
	if err := writeRecoveryManifest(filepath.Join(workDir, manifestName), manifest); err != nil {
		return err
	}
	bundle, err := os.CreateTemp("", "sitectl-isle-recovery-*.tar.gz")
	if err != nil {
		return err
	}
	bundlePath := bundle.Name()
	if err := bundle.Close(); err != nil {
		return err
	}
	defer os.Remove(bundlePath)
	if err := createRecoveryBundle(workDir, bundlePath, manifest); err != nil {
		return err
	}
	if err := ctx.UploadFile(bundlePath, outputPath); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Recovery bundle written to %s\n", outputPath)
	return nil
}

// RunRecoveryRestore validates a bundle completely before replacing authoritative state.
func RunRecoveryRestore(cmd *cobra.Command, ctx *config.Context, inputPath string, yolo bool) (retErr error) {
	workDir, manifest, err := stageAndValidateRecoveryBundle(ctx, inputPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	ok, err := corejob.ConfirmDatabaseReplacement(ctx.Name, "ISLE authoritative state", inputPath, yolo)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("recovery restore cancelled")
	}

	cli, err := docker.GetDockerCli(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()
	containers, err := recoveryContainers(cmd.Context(), cli, ctx)
	if err != nil {
		return err
	}
	if manifest.FcrepoEnabled != (containers.fcrepo != "") {
		return fmt.Errorf("bundle Fcrepo topology (%t) does not match the target context (%t)", manifest.FcrepoEnabled, containers.fcrepo != "")
	}
	resume, err := quiesceISLE(cmd, ctx, cli, containers)
	if err != nil {
		return err
	}
	restoreComplete := false
	defer func() { retErr = errors.Join(retErr, finalizeRecoveryRestore(restoreComplete, resume)) }()

	if err := importDatabase(cmd.Context(), cli, containers.mariadb, "drupal_default", "drupal_default", ctx, "DRUPAL_DEFAULT_DB_PASSWORD", filepath.Join(workDir, drupalDatabaseName), cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("restore Drupal database: %w", err)
	}
	if err := replaceContainerPaths(cmd.Context(), cli, containers.drupal, "/var/www/drupal", []string{"web/sites/default/files", "private"}, filepath.Join(workDir, drupalFilesName), cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("restore Drupal files: %w", err)
	}
	if manifest.FcrepoEnabled {
		if err := importDatabase(cmd.Context(), cli, containers.mariadb, "fcrepo", "fcrepo", ctx, "FCREPO_DB_PASSWORD", filepath.Join(workDir, fcrepoDatabaseName), cmd.ErrOrStderr()); err != nil {
			return fmt.Errorf("restore Fcrepo database: %w", err)
		}
		if err := replaceContainerPaths(cmd.Context(), cli, containers.fcrepo, "/", []string{"data"}, filepath.Join(workDir, fcrepoDataName), cmd.ErrOrStderr()); err != nil {
			return fmt.Errorf("restore Fcrepo data: %w", err)
		}
	}
	if _, err := docker.ExecCapture(cmd.Context(), cli, containers.drupal, ctx.EffectiveDrupalContainerRoot(), []string{"drush", "cr", "-y"}); err != nil {
		return fmt.Errorf("rebuild Drupal caches: %w", err)
	}
	restoreComplete = true
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Authoritative state restored. Rebuild Solr/Blazegraph indexes and replay required derivative jobs, then run sitectl healthcheck and sitectl verify --strict.")
	return nil
}

func finalizeRecoveryRestore(complete bool, resume func() error) error {
	if !complete {
		return fmt.Errorf("restore did not complete; Drupal remains in maintenance mode and ingress and mutation consumers remain stopped to protect partially restored state")
	}
	return resume()
}

// ValidateRecoveryBundle downloads and verifies a bundle without changing the site.
func ValidateRecoveryBundle(ctx *config.Context, inputPath string) (RecoveryManifest, error) {
	workDir, manifest, err := stageAndValidateRecoveryBundle(ctx, inputPath)
	if workDir != "" {
		defer os.RemoveAll(workDir)
	}
	return manifest, err
}

type recoveryContainerSet struct {
	drupal  string
	mariadb string
	fcrepo  string
	paused  []string
}

func recoveryContainers(runCtx context.Context, cli *docker.DockerClient, ctx *config.Context) (recoveryContainerSet, error) {
	var result recoveryContainerSet
	var err error
	if result.drupal, err = cli.GetContainerNameContext(runCtx, ctx, "drupal"); err != nil {
		return result, fmt.Errorf("find running drupal container: %w", err)
	}
	if result.drupal == "" {
		return result, fmt.Errorf("find running drupal container: no container found")
	}
	if result.mariadb, err = cli.GetContainerNameContext(runCtx, ctx, "mariadb"); err != nil {
		return result, fmt.Errorf("find running mariadb container: %w", err)
	}
	if result.mariadb == "" {
		return result, fmt.Errorf("find running mariadb container: no container found")
	}
	result.fcrepo, err = cli.GetContainerNameContext(runCtx, ctx, "fcrepo")
	if err != nil {
		return result, fmt.Errorf("detect fcrepo container: %w", err)
	}
	for _, service := range []string{"traefik", "alpaca", "milliner", "mergepdf", "crayfits", "homarus", "houdini", "hypercube"} {
		name, lookupErr := cli.GetContainerNameContext(runCtx, ctx, service)
		if lookupErr != nil {
			return result, fmt.Errorf("detect %s container: %w", service, lookupErr)
		}
		if name != "" {
			result.paused = append(result.paused, service)
		}
	}
	return result, nil
}

func quiesceISLE(cmd *cobra.Command, ctx *config.Context, cli *docker.DockerClient, containers recoveryContainerSet) (func() error, error) {
	workingDir := ctx.EffectiveDrupalContainerRoot()
	if _, err := docker.ExecCapture(cmd.Context(), cli, containers.drupal, workingDir, []string{"drush", "state:set", "system.maintenance_mode", "1", "-y"}); err != nil {
		return nil, fmt.Errorf("enable Drupal maintenance mode: %w", err)
	}
	if _, err := docker.ExecCapture(cmd.Context(), cli, containers.drupal, workingDir, []string{"drush", "cr", "-y"}); err != nil {
		_, _ = docker.ExecCapture(context.Background(), cli, containers.drupal, workingDir, []string{"drush", "state:set", "system.maintenance_mode", "0", "-y"})
		return nil, fmt.Errorf("rebuild cache for maintenance mode: %w", err)
	}
	if err := runComposeServices(cmd.Context(), ctx, "stop", containers.paused); err != nil {
		_, _ = docker.ExecCapture(context.Background(), cli, containers.drupal, workingDir, []string{"drush", "state:set", "system.maintenance_mode", "0", "-y"})
		_, _ = docker.ExecCapture(context.Background(), cli, containers.drupal, workingDir, []string{"drush", "cr", "-y"})
		_ = runComposeServices(context.Background(), ctx, "up", containers.paused)
		return nil, fmt.Errorf("stop mutation-producing services: %w", err)
	}
	return func() error {
		var resumeErr error
		if _, err := docker.ExecCapture(context.Background(), cli, containers.drupal, workingDir, []string{"drush", "state:set", "system.maintenance_mode", "0", "-y"}); err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("disable Drupal maintenance mode: %w", err))
		}
		if _, err := docker.ExecCapture(context.Background(), cli, containers.drupal, workingDir, []string{"drush", "cr", "-y"}); err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("rebuild cache after maintenance: %w", err))
		}
		if err := runComposeServices(context.Background(), ctx, "up", containers.paused); err != nil {
			resumeErr = errors.Join(resumeErr, fmt.Errorf("restart paused services: %w", err))
		}
		return resumeErr
	}, nil
}

func runComposeServices(runCtx context.Context, ctx *config.Context, action string, services []string) error {
	if len(services) == 0 {
		return nil
	}
	args := []string{"compose"}
	args = append(args, ctx.DockerComposeGlobalArgsForCommand(action)...)
	args = append(args, action)
	if action == "up" {
		args = append(args, "-d")
	}
	args = append(args, services...)
	_, err := ctx.RunQuietCommandContext(runCtx, exec.Command("docker", args...)) // #nosec G204 -- executable and action are fixed; services are curated constants.
	return err
}

func dumpDatabase(runCtx context.Context, cli *docker.DockerClient, container, database, user string, ctx *config.Context, secretName, destination string, stderr io.Writer) error {
	password, err := ctx.ReadSmallFile(filepath.Join(ctx.ProjectDir, "secrets", secretName))
	if err != nil {
		return err
	}
	file, err := os.Create(destination) // #nosec G304 -- destination is inside a process-owned temporary directory.
	if err != nil {
		return err
	}
	defer file.Close()
	compressed := gzip.NewWriter(file)
	exitCode, execErr := cli.Exec(runCtx, docker.ExecOptions{Container: container, Cmd: []string{"mysqldump", "--single-transaction", "--routines", "--triggers", "-u", user, database}, Env: []string{"MYSQL_PWD=" + strings.TrimSpace(password)}, AttachStdout: true, AttachStderr: true, Stdout: compressed, Stderr: stderr})
	closeErr := compressed.Close()
	if execErr != nil {
		return execErr
	}
	if exitCode != 0 {
		return fmt.Errorf("mysqldump exited with code %d", exitCode)
	}
	return closeErr
}

func importDatabase(runCtx context.Context, cli *docker.DockerClient, container, database, user string, ctx *config.Context, secretName, source string, stderr io.Writer) error {
	password, err := ctx.ReadSmallFile(filepath.Join(ctx.ProjectDir, "secrets", secretName))
	if err != nil {
		return err
	}
	file, err := os.Open(source) // #nosec G304 -- source is a validated artifact in a process-owned temporary directory.
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	exitCode, err := cli.Exec(runCtx, docker.ExecOptions{Container: container, Cmd: []string{"mysql", "-u", user, database}, Env: []string{"MYSQL_PWD=" + strings.TrimSpace(password)}, AttachStdin: true, AttachStderr: true, Stdin: compressed, Stdout: io.Discard, Stderr: stderr})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("mysql exited with code %d", exitCode)
	}
	return nil
}

func archiveContainerPaths(runCtx context.Context, cli *docker.DockerClient, container, workingDir string, paths []string, destination string, stderr io.Writer) error {
	file, err := os.Create(destination) // #nosec G304 -- destination is inside a process-owned temporary directory.
	if err != nil {
		return err
	}
	defer file.Close()
	argv := append([]string{"tar", "-czf", "-", "--numeric-owner"}, paths...)
	exitCode, err := cli.Exec(runCtx, docker.ExecOptions{Container: container, Cmd: argv, WorkingDir: workingDir, AttachStdout: true, AttachStderr: true, Stdout: file, Stderr: stderr})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("tar exited with code %d", exitCode)
	}
	return file.Close()
}

func replaceContainerPaths(runCtx context.Context, cli *docker.DockerClient, container, workingDir string, paths []string, source string, stderr io.Writer) error {
	findArgs := append(append([]string{"find"}, paths...), "-mindepth", "1", "-delete")
	if _, err := docker.ExecCapture(runCtx, cli, container, workingDir, findArgs); err != nil {
		return fmt.Errorf("clear existing data: %w", err)
	}
	file, err := os.Open(source) // #nosec G304 -- source is a validated artifact in a process-owned temporary directory.
	if err != nil {
		return err
	}
	defer file.Close()
	exitCode, err := cli.Exec(runCtx, docker.ExecOptions{Container: container, Cmd: []string{"tar", "-xzf", "-", "--numeric-owner"}, WorkingDir: workingDir, AttachStdin: true, AttachStderr: true, Stdin: file, Stdout: io.Discard, Stderr: stderr})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("tar restore exited with code %d", exitCode)
	}
	return nil
}

func requiredRecoveryArtifacts(fcrepo bool) []string {
	names := []string{drupalDatabaseName, drupalFilesName}
	if fcrepo {
		names = append(names, fcrepoDatabaseName, fcrepoDataName)
	}
	return names
}

func writeRecoveryManifest(path string, manifest RecoveryManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600) // #nosec G306 -- recovery manifests are operator-private artifacts.
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) // #nosec G304 -- callers constrain paths to recovery staging directories.
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func createRecoveryBundle(workDir, output string, manifest RecoveryManifest) error {
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- output is a process-owned temporary file.
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	names := append([]string{manifestName}, requiredRecoveryArtifacts(manifest.FcrepoEnabled)...)
	for _, name := range names {
		data, err := os.Open(filepath.Join(workDir, name)) // #nosec G304 -- name comes from the fixed recovery contract.
		if err != nil {
			return err
		}
		info, err := data.Stat()
		if err != nil {
			_ = data.Close()
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: manifest.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
			_ = data.Close()
			return err
		}
		if _, err := io.Copy(tw, data); err != nil {
			_ = data.Close()
			return err
		}
		if err := data.Close(); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func stageAndValidateRecoveryBundle(ctx *config.Context, inputPath string) (string, RecoveryManifest, error) {
	if ctx == nil {
		return "", RecoveryManifest{}, fmt.Errorf("context is nil")
	}
	bundle, err := os.CreateTemp("", "sitectl-isle-recovery-download-*.tar.gz")
	if err != nil {
		return "", RecoveryManifest{}, err
	}
	bundlePath := bundle.Name()
	if err := bundle.Close(); err != nil {
		return "", RecoveryManifest{}, err
	}
	defer os.Remove(bundlePath)
	if err := corejob.DownloadContextFile(ctx, inputPath, bundlePath); err != nil {
		return "", RecoveryManifest{}, err
	}
	workDir, err := os.MkdirTemp("", "sitectl-isle-recovery-validate-*")
	if err != nil {
		return "", RecoveryManifest{}, err
	}
	if err := extractRecoveryBundle(bundlePath, workDir); err != nil {
		_ = os.RemoveAll(workDir)
		return "", RecoveryManifest{}, err
	}
	manifest, err := validateRecoveryDirectory(workDir)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", RecoveryManifest{}, err
	}
	return workDir, manifest, nil
}

func extractRecoveryBundle(bundlePath, destination string) error {
	file, err := os.Open(bundlePath) // #nosec G304 -- bundlePath is a process-owned download staging file.
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if !recoveryAllowedFiles[name] || name != header.Name || seen[name] || header.Typeflag != tar.TypeReg {
			return fmt.Errorf("recovery bundle contains invalid entry %q", header.Name)
		}
		seen[name] = true
		output, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(output, tr, header.Size); err != nil {
			_ = output.Close()
			return err
		}
		if err := output.Close(); err != nil {
			return err
		}
	}
	return nil
}

func validateRecoveryDirectory(workDir string) (RecoveryManifest, error) {
	data, err := os.ReadFile(filepath.Join(workDir, manifestName)) // #nosec G304 -- workDir is process-owned.
	if err != nil {
		return RecoveryManifest{}, fmt.Errorf("read recovery manifest: %w", err)
	}
	var manifest RecoveryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("parse recovery manifest: %w", err)
	}
	if manifest.FormatVersion != recoveryFormatVersion {
		return manifest, fmt.Errorf("unsupported recovery format version %d", manifest.FormatVersion)
	}
	if manifest.CreatedAt.IsZero() || len(manifest.Authoritative) == 0 || len(manifest.Rebuildable) == 0 || len(manifest.RequiredExternal) == 0 || strings.TrimSpace(manifest.ExcludedSecrets) == "" {
		return manifest, fmt.Errorf("recovery manifest is missing state-boundary metadata")
	}
	required := requiredRecoveryArtifacts(manifest.FcrepoEnabled)
	if len(manifest.Artifacts) != len(required) {
		return manifest, fmt.Errorf("manifest artifact set does not match the recovery contract")
	}
	for _, name := range required {
		expected := manifest.Artifacts[name]
		if expected == "" {
			return manifest, fmt.Errorf("manifest is missing checksum for %s", name)
		}
		actual, err := fileSHA256(filepath.Join(workDir, name))
		if err != nil {
			return manifest, fmt.Errorf("read recovery artifact %s: %w", name, err)
		}
		if actual != expected {
			return manifest, fmt.Errorf("recovery artifact %s checksum mismatch", name)
		}
	}
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return manifest, err
	}
	if len(entries) != len(required)+1 {
		return manifest, fmt.Errorf("recovery bundle contains files outside the manifest contract")
	}
	for _, entry := range entries {
		if entry.IsDir() || (entry.Name() != manifestName && manifest.Artifacts[entry.Name()] == "") {
			return manifest, fmt.Errorf("recovery bundle contains unmanifested file %s", entry.Name())
		}
	}
	return manifest, nil
}

// FormatRecoveryManifest returns a deterministic operator-facing bundle summary.
func FormatRecoveryManifest(manifest RecoveryManifest) string {
	authoritative := append([]string(nil), manifest.Authoritative...)
	rebuildable := append([]string(nil), manifest.Rebuildable...)
	external := append([]string(nil), manifest.RequiredExternal...)
	sort.Strings(authoritative)
	sort.Strings(rebuildable)
	sort.Strings(external)
	return fmt.Sprintf("format: %d\ncreated: %s\nfcrepo: %t\nauthoritative: %s\nrebuildable: %s\nrequired external: %s\n", manifest.FormatVersion, manifest.CreatedAt.Format(time.RFC3339), manifest.FcrepoEnabled, strings.Join(authoritative, ", "), strings.Join(rebuildable, ", "), strings.Join(external, ", "))
}
