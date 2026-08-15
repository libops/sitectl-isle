package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libops/sitectl-isle/pkg/workbench"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

func TestWorkbenchReconcileSupplementalJobMergesStandardArtifacts(t *testing.T) {
	t.Parallel()
	inputDir := t.TempDir()
	writeFixture := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(inputDir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture("target.csv", "id,title\na,One\nb,Two\n")
	writeFixture("rollback.csv", "node_id\n100\n101\n")
	writeFixture("target.add_media.csv", "node_id,file\n100,/media/original.tif\n")
	writeFixture("target.pending_supplemental.csv", "id,node_id,file,media_use_tid,published\na,,/media/public.pdf,,\n")
	writeFixture("target.unpublished_supplemental.csv", "id,node_id,file,media_use_tid,published\nb,,/media/private.pdf,88,0\n")
	contractPath := writeCrosswalkManifest(t, inputDir, []string{
		"target.csv",
		"target.add_media.csv",
		"target.pending_supplemental.csv",
		"target.unpublished_supplemental.csv",
	}, &workbench.ArtifactPolicy{
		SupplementalMediaUseTID:            "151326",
		PendingSupplementalPublished:       "1",
		UnpublishedSupplementalMediaUseTID: "151326",
		UnpublishedSupplementalPublished:   "0",
	})

	command := &cobra.Command{}
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	job := &workbenchReconcileSupplementalJob{
		InputDir:    filepath.ToSlash(inputDir),
		RollbackCSV: filepath.ToSlash(filepath.Join(inputDir, "rollback.csv")),
		Contract:    contractPath,
	}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: inputDir}
	if err := job.Run(command, ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(inputDir, "target.add_media.csv"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"node_id,file,media_use_tid,published",
		"100,/media/original.tif,,1",
		"100,/media/public.pdf,151326,1",
		"101,/media/private.pdf,88,0",
		"",
	}, "\n")
	if string(data) != want {
		t.Fatalf("reconciled CSV = %q, want %q", data, want)
	}
	if !strings.Contains(stdout.String(), "3 deduplicated media rows") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func writeCrosswalkManifest(t *testing.T, directory string, names []string, policy *workbench.ArtifactPolicy) string {
	t.Helper()
	manifest := workbench.ArtifactManifest{
		Version: workbench.CrosswalkArtifactManifestVersion,
		Spec: workbench.ArtifactSpec{
			Name:        "test-workbench",
			Version:     workbench.CrosswalkSpecVersion,
			Fingerprint: strings.Repeat("a", 64),
		},
		Policy:    policy,
		Artifacts: make([]workbench.ArtifactDescriptor, 0, len(names)),
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		reader := csv.NewReader(bytes.NewReader(data))
		rows, err := reader.ReadAll()
		if err != nil || len(rows) == 0 {
			t.Fatalf("read test artifact %s: rows=%d error=%v", name, len(rows), err)
		}
		digest := sha256.Sum256(data)
		manifest.Artifacts = append(manifest.Artifacts, workbench.ArtifactDescriptor{
			Path: name, MediaType: "text/csv; charset=utf-8",
			SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)), CSVRows: len(rows) - 1,
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, workbench.CrosswalkArtifactManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return writeCrosswalkContract(t, workbench.ArtifactContract{
		Version: workbench.CrosswalkArtifactContractVersion,
		Spec:    manifest.Spec,
		Policy:  policy,
	})
}

func writeCrosswalkContract(t *testing.T, contract workbench.ArtifactContract) string {
	t.Helper()
	data, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	contractPath := filepath.Join(t.TempDir(), workbench.CrosswalkArtifactContractName)
	if err := os.WriteFile(contractPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(contractPath)
}

func TestWorkbenchReconcileSupplementalJobRequiresManifest(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "target.pending_supplemental.csv"), []byte("node_id,file\n42,/media/item.pdf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contractPath := writeCrosswalkContract(t, workbench.ArtifactContract{
		Version: workbench.CrosswalkArtifactContractVersion,
		Spec: workbench.ArtifactSpec{
			Name: "test-workbench", Version: workbench.CrosswalkSpecVersion, Fingerprint: strings.Repeat("a", 64),
		},
		Policy: &workbench.ArtifactPolicy{SupplementalMediaUseTID: "77", PendingSupplementalPublished: "1"},
	})
	job := &workbenchReconcileSupplementalJob{InputDir: filepath.ToSlash(directory), Contract: contractPath}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	err := job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), workbench.CrosswalkArtifactManifestName) {
		t.Fatalf("missing manifest error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(directory, "target.add_media.csv")); !os.IsNotExist(statErr) {
		t.Fatalf("reconcile wrote output without a manifest: %v", statErr)
	}
}

func TestWorkbenchReconcileSupplementalJobRejectsContractInsideBatch(t *testing.T) {
	t.Parallel()
	directory := filepath.ToSlash(t.TempDir())
	job := &workbenchReconcileSupplementalJob{
		InputDir: directory,
		Contract: path.Join(directory, workbench.CrosswalkArtifactManifestName),
	}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	err := job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), "outside --input-dir") {
		t.Fatalf("in-batch trust anchor error = %v", err)
	}
}

func TestWorkbenchReconcileSupplementalJobRejectsContractResolvingInsideBatch(t *testing.T) {
	t.Parallel()
	inputDir := t.TempDir()
	contractName := workbench.CrosswalkArtifactContractName
	if err := os.WriteFile(filepath.Join(inputDir, contractName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "uploaded-batch")
	if err := os.Symlink(inputDir, link); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}

	job := &workbenchReconcileSupplementalJob{
		InputDir: filepath.ToSlash(inputDir),
		Contract: filepath.ToSlash(filepath.Join(link, contractName)),
	}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: inputDir}
	err := job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), "resolves inside the uploaded batch") {
		t.Fatalf("resolved in-batch trust anchor error = %v", err)
	}
}

func TestWorkbenchReconcileSupplementalJobUsesManifestPolicy(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	pending := "id,node_id,file,media_use_tid,published\n,42,/media/item.pdf,,\n"
	if err := os.WriteFile(filepath.Join(directory, "target.pending_supplemental.csv"), []byte(pending), 0o600); err != nil {
		t.Fatal(err)
	}
	contractPath := writeCrosswalkManifest(t, directory, []string{"target.pending_supplemental.csv"}, &workbench.ArtifactPolicy{
		SupplementalMediaUseTID:      "77",
		PendingSupplementalPublished: "0",
	})

	job := &workbenchReconcileSupplementalJob{InputDir: filepath.ToSlash(directory), Contract: contractPath}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	if err := job.Run(&cobra.Command{}, ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "target.add_media.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "node_id,file,media_use_tid,published\n42,/media/item.pdf,77,0\n"; got != want {
		t.Fatalf("reconciled CSV = %q, want manifest policy %q", got, want)
	}
}

func TestWorkbenchReconcileSupplementalJobRejectsContractMismatch(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "target.pending_supplemental.csv"), []byte("node_id,file,media_use_tid,published\n42,/media/item.pdf,,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contractPath := writeCrosswalkManifest(t, directory, []string{"target.pending_supplemental.csv"}, &workbench.ArtifactPolicy{
		SupplementalMediaUseTID: "77", PendingSupplementalPublished: "1",
	})
	contractData, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var contract workbench.ArtifactContract
	if err := json.Unmarshal(contractData, &contract); err != nil {
		t.Fatal(err)
	}
	contract.Policy.SupplementalMediaUseTID = "88"
	contractData, err = json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, contractData, 0o600); err != nil {
		t.Fatal(err)
	}

	job := &workbenchReconcileSupplementalJob{InputDir: filepath.ToSlash(directory), Contract: contractPath}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	err = job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), "policy does not match the trusted site contract") {
		t.Fatalf("contract policy mismatch error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(directory, "target.add_media.csv")); !os.IsNotExist(statErr) {
		t.Fatalf("reconcile wrote output for an untrusted batch: %v", statErr)
	}
}

func TestWorkbenchReconcileSupplementalJobValidatesEveryManifestedArtifact(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := map[string]string{
		"target.pending_supplemental.csv": "id,node_id,file,media_use_tid,published\n,42,/media/item.pdf,77,1\n",
		"target.update.csv":               "node_id,title\n42,Original\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	contractPath := writeCrosswalkManifest(t, directory, []string{"target.pending_supplemental.csv", "target.update.csv"}, &workbench.ArtifactPolicy{
		SupplementalMediaUseTID:      "77",
		PendingSupplementalPublished: "1",
	})
	// Preserve byte length and CSV row count so only the digest detects this
	// unused artifact's mutation.
	if err := os.WriteFile(filepath.Join(directory, "target.update.csv"), []byte("node_id,title\n42,Tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	job := &workbenchReconcileSupplementalJob{InputDir: filepath.ToSlash(directory), Contract: contractPath}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	err := job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("tampered unused artifact error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(directory, "target.add_media.csv")); !os.IsNotExist(statErr) {
		t.Fatalf("reconcile wrote output after integrity failure: %v", statErr)
	}
}

func TestWorkbenchReconcileSupplementalJobRejectsKnownUnmanifestedArtifact(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "target.pending_supplemental.csv"), []byte("node_id,file,media_use_tid,published\n42,/media/item.pdf,77,1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "agents.csv"), []byte("name\nUnexpected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	contractPath := writeCrosswalkManifest(t, directory, []string{"target.pending_supplemental.csv"}, &workbench.ArtifactPolicy{
		SupplementalMediaUseTID:      "77",
		PendingSupplementalPublished: "1",
	})

	job := &workbenchReconcileSupplementalJob{InputDir: filepath.ToSlash(directory), Contract: contractPath}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: directory}
	err := job.Run(&cobra.Command{}, ctx)
	if err == nil || !strings.Contains(err.Error(), "unmanifested Crosswalk artifact") {
		t.Fatalf("unmanifested artifact error = %v", err)
	}
}

func TestWorkbenchPreflightJobReportsMissingAndUnreadableSeparately(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "available.tif"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(root, "unreadable.tif")
	if err := os.WriteFile(unreadable, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "target.csv")
	if err := os.WriteFile(input, []byte("id,file\n1,available.tif\n2,missing.tif\n3,unreadable.tif\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := &cobra.Command{}
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	job := &workbenchPreflightJob{
		Input:       filepath.ToSlash(input),
		StagingRoot: filepath.ToSlash(root),
		Columns:     workbench.DefaultMediaColumns(),
	}
	ctx := &config.Context{Name: "test", DockerHostType: config.ContextLocal, ProjectDir: root}
	err := job.Run(command, ctx)
	if err == nil || !strings.Contains(err.Error(), "missing=1 unreadable=1 invalid=0") {
		t.Fatalf("error = %v", err)
	}
	if output := stdout.String(); !strings.Contains(output, "missing\trow=3") || !strings.Contains(output, "unreadable\trow=4") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestWorkbenchPreflightDefaultRootsMatchCrosswalkContract(t *testing.T) {
	t.Parallel()
	command := &cobra.Command{}
	job := &workbenchPreflightJob{StagingRoot: "/mnt/islandora_staging"}
	job.BindFlags(command)
	if command.Flags().Changed("allowed-root") {
		t.Fatal("default allowed roots unexpectedly marked changed")
	}
	roots := []string{job.StagingRoot, "/home", "/mnt"}
	inspector := &fixedPathInspector{}
	references := []workbench.MediaReference{
		{Path: "/home/operator/item.tif"},
		{Path: "/mnt/other/item.tif"},
		{Path: "relative/item.tif"},
	}
	results, err := workbench.InspectMediaReferences(references, job.StagingRoot, roots, inspector)
	if err != nil {
		t.Fatal(err)
	}
	for index, result := range results {
		if result.Status != workbench.PathAvailable {
			t.Errorf("result %d = %s (%v), want available", index, result.Status, result.Err)
		}
	}
}

type fixedPathInspector struct{}

func (*fixedPathInspector) Lstat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, ".tif") {
		return workbenchTestFileInfo{mode: 0o600}, nil
	}
	return workbenchTestFileInfo{mode: os.ModeDir | 0o700}, nil
}

func (*fixedPathInspector) Readable(string) error { return nil }

type workbenchTestFileInfo struct{ mode os.FileMode }

func (i workbenchTestFileInfo) Name() string       { return "fixture" }
func (i workbenchTestFileInfo) Size() int64        { return 1 }
func (i workbenchTestFileInfo) Mode() os.FileMode  { return i.mode }
func (i workbenchTestFileInfo) ModTime() time.Time { return time.Time{} }
func (i workbenchTestFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i workbenchTestFileInfo) Sys() any           { return nil }

func TestExactContextPathRejectsURLsRelativeAndNonCanonicalPaths(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "relative.csv", "https://example.org/rollback.csv", "/tmp/../rollback.csv", "/"} {
		if _, err := exactContextPath("--artifact", value); err == nil {
			t.Fatalf("path %q unexpectedly accepted", value)
		}
	}
	if got, err := exactContextPath("--artifact", "/var/lib/workbench/rollback.csv"); err != nil || got != "/var/lib/workbench/rollback.csv" {
		t.Fatalf("canonical path = %q, %v", got, err)
	}
}

func TestValidateWorkbenchExecutionConfigUsesResolvedSiteURL(t *testing.T) {
	t.Parallel()
	ctx := &config.Context{Name: "prod"}
	resolver := func(*cobra.Command, *config.Context) (string, error) {
		return "https://repo.example.org/islandora/", nil
	}
	data := []byte("task: delete\nhost: https://REPO.example.org:443/islandora\n")
	snapshot, cfg, err := validateWorkbenchExecutionConfig(&cobra.Command{}, ctx, resolver, "/configs/delete.yml", data, "delete", "/logs/delete.log")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Task != "delete" || !strings.Contains(string(snapshot), "log_file_path: /logs/delete.log") {
		t.Fatalf("config=%#v snapshot=%q", cfg, snapshot)
	}

	_, _, err = validateWorkbenchExecutionConfig(&cobra.Command{}, ctx, resolver, "/configs/delete.yml", []byte("task: delete\nhost: https://repo.example.org/other-site\n"), "delete", "/logs/delete.log")
	if err == nil || !strings.Contains(err.Error(), "does not match context") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestSnapshotContextArtifactIsImmutableCopyAndRemoves(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx := &config.Context{Name: "local", DockerHostType: config.ContextLocal, ProjectDir: root}
	source := filepath.Join(root, "rollback.csv")
	snapshot, remove, err := snapshotContextArtifact(context.Background(), ctx, source, ".csv", []byte("node_id\n42\n"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == source || filepath.Dir(snapshot) != root || !strings.Contains(filepath.Base(snapshot), ".sitectl-workbench-snapshot-") {
		t.Fatalf("snapshot path = %q", snapshot)
	}
	data, err := os.ReadFile(snapshot)
	if err != nil || string(data) != "node_id\n42\n" {
		t.Fatalf("snapshot data = %q, %v", data, err)
	}
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("snapshot still exists: %v", err)
	}
}
