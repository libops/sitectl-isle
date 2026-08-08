package jobs

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveryBundleRoundTripAndChecksumValidation(t *testing.T) {
	source := t.TempDir()
	manifest := RecoveryManifest{
		FormatVersion:    recoveryFormatVersion,
		CreatedAt:        time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC),
		FcrepoEnabled:    true,
		Artifacts:        map[string]string{},
		Authoritative:    []string{"Drupal database", "Fcrepo object data"},
		Rebuildable:      []string{"Solr indexes"},
		RequiredExternal: []string{"site Git checkout", "Vault backup"},
		ExcludedSecrets:  "Secrets are excluded.",
	}
	for _, name := range requiredRecoveryArtifacts(true) {
		if err := os.WriteFile(filepath.Join(source, name), []byte("fixture:"+name), 0o600); err != nil {
			t.Fatal(err)
		}
		digest, err := fileSHA256(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		manifest.Artifacts[name] = digest
	}
	if err := writeRecoveryManifest(filepath.Join(source, manifestName), manifest); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "recovery.tar.gz")
	if err := os.WriteFile(bundle, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createRecoveryBundle(source, bundle, manifest); err != nil {
		t.Fatal(err)
	}
	extracted := t.TempDir()
	if err := extractRecoveryBundle(bundle, extracted); err != nil {
		t.Fatal(err)
	}
	got, err := validateRecoveryDirectory(extracted)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FcrepoEnabled || len(got.Artifacts) != 4 {
		t.Fatalf("unexpected manifest: %#v", got)
	}

	if err := os.WriteFile(filepath.Join(extracted, drupalDatabaseName), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRecoveryDirectory(extracted); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered bundle error = %v, want checksum mismatch", err)
	}
}

func TestExtractRecoveryBundleRejectsTraversalAndUnknownEntries(t *testing.T) {
	for _, name := range []string{"../escape", "unknown.txt", "nested/manifest.json"} {
		t.Run(name, func(t *testing.T) {
			bundle := filepath.Join(t.TempDir(), "malicious.tar.gz")
			file, err := os.Create(bundle)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			payload := []byte("bad")
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if err := extractRecoveryBundle(bundle, t.TempDir()); err == nil {
				t.Fatal("malicious archive unexpectedly accepted")
			}
		})
	}
}

func TestFormatRecoveryManifestIsDeterministic(t *testing.T) {
	manifest := RecoveryManifest{
		FormatVersion:    1,
		CreatedAt:        time.Date(2026, 8, 7, 1, 2, 3, 0, time.UTC),
		Authoritative:    []string{"z", "a"},
		Rebuildable:      []string{"y", "b"},
		RequiredExternal: []string{"vault"},
		ExcludedSecrets:  "excluded",
	}
	got := FormatRecoveryManifest(manifest)
	if !strings.Contains(got, "authoritative: a, z") || !strings.Contains(got, "rebuildable: b, y") {
		t.Fatalf("unexpected summary:\n%s", got)
	}
}

func TestFinalizeRecoveryRestoreFailsClosedUntilRestoreCompletes(t *testing.T) {
	t.Parallel()

	resumed := false
	resume := func() error {
		resumed = true
		return nil
	}
	if err := finalizeRecoveryRestore(false, resume); err == nil || !strings.Contains(err.Error(), "remains in maintenance mode") {
		t.Fatalf("incomplete restore error = %v, want fail-closed recovery guidance", err)
	}
	if resumed {
		t.Fatal("incomplete restore reopened the site")
	}
	if err := finalizeRecoveryRestore(true, resume); err != nil {
		t.Fatalf("complete restore finalization error = %v", err)
	}
	if !resumed {
		t.Fatal("complete restore did not reopen the site")
	}
}
