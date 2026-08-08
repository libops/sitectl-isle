package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
)

func TestValidateDatabaseSecretBoundaryAcceptsOneShotInitializers(t *testing.T) {
	ctx := writeDatabaseBoundaryFixture(t, false)
	result := validateDatabaseSecretBoundary(ctx)
	if result.Status != sitevalidate.StatusOK {
		t.Fatalf("expected valid boundary, got %#v", result)
	}
}

func TestValidateDatabaseSecretBoundaryRejectsRootOnLongRunningServices(t *testing.T) {
	ctx := writeDatabaseBoundaryFixture(t, true)
	result := validateDatabaseSecretBoundary(ctx)
	if result.Status != sitevalidate.StatusFailed {
		t.Fatalf("expected failed boundary, got %#v", result)
	}
	for _, want := range []string{"drupal receives DB_ROOT_PASSWORD", "fcrepo receives DB_ROOT_PASSWORD"} {
		if !strings.Contains(result.Detail, want) {
			t.Fatalf("missing %q in %q", want, result.Detail)
		}
	}
}

func writeDatabaseBoundaryFixture(t *testing.T, leakRoot bool) *config.Context {
	t.Helper()
	projectDir := t.TempDir()
	leaked := ""
	if leakRoot {
		leaked = "\n      - source: DB_ROOT_PASSWORD"
	}
	compose := `services:
  mariadb:
    secrets:
      - source: DB_ROOT_PASSWORD
  database-init:
    secrets:
      - source: DB_ROOT_PASSWORD
      - source: DRUPAL_DEFAULT_DB_PASSWORD
  drupal:
    depends_on:
      database-init:
        condition: service_completed_successfully
    secrets:
      - source: DRUPAL_DEFAULT_DB_PASSWORD` + leaked + `
  fcrepo-database-init:
    secrets:
      - source: DB_ROOT_PASSWORD
      - source: FCREPO_DB_PASSWORD
  fcrepo:
    depends_on:
      fcrepo-database-init:
        condition: service_completed_successfully
    secrets:
      - source: FCREPO_DB_PASSWORD` + leaked + "\n"
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal}
}
