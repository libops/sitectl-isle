package jobs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

const retryableWorkbenchLog = `2026-01-01 INFO starting
2026-01-01 ERROR - File not created for "/mnt/islandora_staging/item.tif", POST request to "/file/upload/media/file/field_media_file?_format=json" returned an HTTP status code of "504" and a response body of b''.
2026-01-01 ERROR - Media not created, POST request to "/entity/media" returned an HTTP status code of "422" and a response body of b'bad'.
2026-01-01 ERROR - JSON request body used in previous POST to "/entity/media" was {'field_media_file': [{'target_id': False}], 'field_media_of': [{'target_id': 436648}], 'field_media_use': [{'target_id': 16}], 'status': {'value': '1'}}.
2026-01-01 ERROR - Media for /mnt/islandora_staging/item.tif not created (HTTP respone code 422).
`

func TestWorkbenchRetryMediaJobRunBindsFlagsAndOrchestratesSnapshots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	failedLog := writeWorkbenchJobFixture(t, root, "failed.log", retryableWorkbenchLog)
	configPath := writeWorkbenchJobFixture(t, root, "add-media.yml", "task: add_media\nhost: https://repo.example.org/islandora\n")
	workbenchPath := writeWorkbenchJobFixture(t, root, "workbench", "test fixture\n")
	outputPath := filepath.Join(root, "retry.csv")
	retryLog := filepath.Join(root, "retry.log")

	job := &workbenchRetryMediaJob{resolveDrupal: matchingWorkbenchDrupalEndpoint}
	command := &cobra.Command{}
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	job.BindFlags(command)
	parseWorkbenchJobFlags(t, command,
		"--failed-log", failedLog,
		"--output", outputPath,
		"--config", configPath,
		"--workbench", workbenchPath,
		"--retry-log", retryLog,
	)
	if job.FailedLog != failedLog || job.Output != outputPath || job.Config != configPath || job.Workbench != workbenchPath || job.RetryLog != retryLog {
		t.Fatalf("bound retry job = %#v", job)
	}

	runs := 0
	job.run = func(gotCommand *cobra.Command, gotContext *config.Context, gotWorkbench, configSnapshot, csvSnapshot, gotLog string) error {
		runs++
		if gotCommand != command || gotContext.Name != "local" || gotWorkbench != workbenchPath || gotLog != retryLog {
			t.Fatalf("runner arguments: command=%p context=%#v workbench=%q log=%q", gotCommand, gotContext, gotWorkbench, gotLog)
		}
		if configSnapshot == configPath || csvSnapshot == outputPath {
			t.Fatalf("runner received mutable inputs: config=%q csv=%q", configSnapshot, csvSnapshot)
		}
		configData := readWorkbenchJobFixture(t, configSnapshot)
		if !strings.Contains(configData, "task: add_media") ||
			!strings.Contains(configData, "log_file_path: "+retryLog) ||
			!strings.Contains(configData, "log_file_mode: w") {
			t.Fatalf("guarded config snapshot = %q", configData)
		}
		const wantCSV = "node_id,file,media_use_tid,published\n436648,/mnt/islandora_staging/item.tif,16,1\n"
		if gotCSV := readWorkbenchJobFixture(t, csvSnapshot); gotCSV != wantCSV {
			t.Fatalf("retry snapshot = %q, want %q", gotCSV, wantCSV)
		}
		return os.WriteFile(retryLog, []byte("2026-01-01 - INFO - Media for \"/mnt/islandora_staging/item.tif\" created and added to https://repo.example.org/islandora/node/436648.\n"), 0o600)
	}

	ctx := localWorkbenchJobContext(root)
	if err := job.Run(command, ctx); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("runner called %d times, want 1", runs)
	}
	const wantCSV = "node_id,file,media_use_tid,published\n436648,/mnt/islandora_staging/item.tif,16,1\n"
	if gotCSV := readWorkbenchJobFixture(t, outputPath); gotCSV != wantCSV {
		t.Fatalf("uploaded retry CSV = %q, want %q", gotCSV, wantCSV)
	}
	if !strings.Contains(stdout.String(), "media retry succeeded for 1 deduplicated rows") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertNoWorkbenchJobSnapshots(t, root)
}

func TestWorkbenchRetryMediaJobRunPropagatesRunnerError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	failedLog := writeWorkbenchJobFixture(t, root, "failed.log", retryableWorkbenchLog)
	configPath := writeWorkbenchJobFixture(t, root, "add-media.yml", "task: add_media\nhost: https://repo.example.org/islandora\n")
	workbenchPath := writeWorkbenchJobFixture(t, root, "workbench", "test fixture\n")
	outputPath := filepath.Join(root, "retry.csv")
	retryLog := filepath.Join(root, "retry.log")
	wantErr := errors.New("runner failed")

	job := &workbenchRetryMediaJob{
		FailedLog:     failedLog,
		Output:        outputPath,
		Config:        configPath,
		Workbench:     workbenchPath,
		RetryLog:      retryLog,
		resolveDrupal: matchingWorkbenchDrupalEndpoint,
		run: func(*cobra.Command, *config.Context, string, string, string, string) error {
			return wantErr
		},
	}
	err := job.Run(&cobra.Command{}, localWorkbenchJobContext(root))
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "workbench media retry failed") {
		t.Fatalf("runner error = %v, want wrapped %v", err, wantErr)
	}
	if gotCSV := readWorkbenchJobFixture(t, outputPath); gotCSV != "node_id,file,media_use_tid,published\n436648,/mnt/islandora_staging/item.tif,16,1\n" {
		t.Fatalf("durable retry CSV after runner failure = %q", gotCSV)
	}
	assertNoWorkbenchJobSnapshots(t, root)
}

func TestWorkbenchRollbackJobRunRequiresConfirmation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rollbackPath := writeWorkbenchJobFixture(t, root, "rollback.csv", "node_id\n42\n77\n")
	configPath := writeWorkbenchJobFixture(t, root, "delete.yml", "task: delete\nhost: https://repo.example.org/islandora\n")
	workbenchPath := writeWorkbenchJobFixture(t, root, "workbench", "test fixture\n")
	logPath := filepath.Join(root, "rollback.log")

	job := &workbenchRollbackJob{resolveDrupal: matchingWorkbenchDrupalEndpoint}
	command := &cobra.Command{}
	job.BindFlags(command)
	parseWorkbenchJobFlags(t, command,
		"--rollback-csv", rollbackPath,
		"--config", configPath,
		"--workbench", workbenchPath,
		"--log", logPath,
	)
	if job.RollbackCSV != rollbackPath || job.Config != configPath || job.Workbench != workbenchPath || job.Log != logPath || job.Yolo {
		t.Fatalf("bound rollback job = %#v", job)
	}

	confirmations := 0
	job.confirm = func(contextName, gotRollbackPath string, nodeCount int, yolo bool) (bool, error) {
		confirmations++
		if contextName != "local" || gotRollbackPath != rollbackPath || nodeCount != 2 || yolo {
			t.Fatalf("confirmation arguments: context=%q rollback=%q nodes=%d yolo=%t", contextName, gotRollbackPath, nodeCount, yolo)
		}
		return false, nil
	}
	runs := 0
	job.run = func(*cobra.Command, *config.Context, string, string, string, string) error {
		runs++
		return nil
	}

	err := job.Run(command, localWorkbenchJobContext(root))
	if err == nil || !strings.Contains(err.Error(), "workbench rollback cancelled") {
		t.Fatalf("cancellation error = %v", err)
	}
	if confirmations != 1 || runs != 0 {
		t.Fatalf("confirmations=%d runner calls=%d, want 1 and 0", confirmations, runs)
	}
	if _, err := os.Lstat(logPath); !os.IsNotExist(err) {
		t.Fatalf("cancelled rollback log exists: %v", err)
	}
	assertNoWorkbenchJobSnapshots(t, root)
}

func TestWorkbenchRollbackJobRunBindsYoloAndOrchestratesSnapshots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rollbackPath := writeWorkbenchJobFixture(t, root, "rollback.csv", "node_id\n42\n77\n")
	configPath := writeWorkbenchJobFixture(t, root, "delete.yml", "task: delete\nhost: https://repo.example.org/islandora\n")
	workbenchPath := writeWorkbenchJobFixture(t, root, "workbench", "test fixture\n")
	logPath := filepath.Join(root, "rollback.log")

	job := &workbenchRollbackJob{resolveDrupal: matchingWorkbenchDrupalEndpoint}
	command := &cobra.Command{}
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	job.BindFlags(command)
	parseWorkbenchJobFlags(t, command,
		"--rollback-csv", rollbackPath,
		"--config", configPath,
		"--workbench", workbenchPath,
		"--log", logPath,
		"--yolo",
	)
	if !job.Yolo {
		t.Fatal("--yolo was not bound to the rollback job")
	}

	runs := 0
	job.run = func(gotCommand *cobra.Command, gotContext *config.Context, gotWorkbench, configSnapshot, rollbackSnapshot, gotLog string) error {
		runs++
		if gotCommand != command || gotContext.Name != "local" || gotWorkbench != workbenchPath || gotLog != logPath {
			t.Fatalf("runner arguments: command=%p context=%#v workbench=%q log=%q", gotCommand, gotContext, gotWorkbench, gotLog)
		}
		if configSnapshot == configPath || rollbackSnapshot == rollbackPath {
			t.Fatalf("runner received mutable inputs: config=%q csv=%q", configSnapshot, rollbackSnapshot)
		}
		configData := readWorkbenchJobFixture(t, configSnapshot)
		if !strings.Contains(configData, "task: delete") ||
			!strings.Contains(configData, "log_file_path: "+logPath) ||
			!strings.Contains(configData, "log_file_mode: w") {
			t.Fatalf("guarded config snapshot = %q", configData)
		}
		if gotCSV := readWorkbenchJobFixture(t, rollbackSnapshot); gotCSV != "node_id\n42\n77\n" {
			t.Fatalf("rollback snapshot = %q", gotCSV)
		}
		return os.WriteFile(logPath, []byte("2026-01-01 - INFO - Node https://repo.example.org/islandora/node/42 deleted.\n2026-01-01 - INFO - Node https://repo.example.org/islandora/node/77 deleted.\n"), 0o600)
	}

	if err := job.Run(command, localWorkbenchJobContext(root)); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("runner calls=%d, want 1", runs)
	}
	if !strings.Contains(stdout.String(), "rollback succeeded for 2 nodes") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	assertNoWorkbenchJobSnapshots(t, root)
}

func TestWorkbenchRollbackJobRunPropagatesRunnerError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rollbackPath := writeWorkbenchJobFixture(t, root, "rollback.csv", "node_id\n42\n")
	configPath := writeWorkbenchJobFixture(t, root, "delete.yml", "task: delete\nhost: https://repo.example.org/islandora\n")
	workbenchPath := writeWorkbenchJobFixture(t, root, "workbench", "test fixture\n")
	logPath := filepath.Join(root, "rollback.log")
	wantErr := errors.New("runner failed")

	job := &workbenchRollbackJob{
		RollbackCSV:   rollbackPath,
		Config:        configPath,
		Workbench:     workbenchPath,
		Log:           logPath,
		Yolo:          true,
		resolveDrupal: matchingWorkbenchDrupalEndpoint,
		run: func(*cobra.Command, *config.Context, string, string, string, string) error {
			return wantErr
		},
	}
	err := job.Run(&cobra.Command{}, localWorkbenchJobContext(root))
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "workbench rollback failed") {
		t.Fatalf("runner error = %v, want wrapped %v", err, wantErr)
	}
	assertNoWorkbenchJobSnapshots(t, root)
}

func matchingWorkbenchDrupalEndpoint(*cobra.Command, *config.Context) (string, error) {
	return "https://repo.example.org/islandora", nil
}

func localWorkbenchJobContext(root string) *config.Context {
	return &config.Context{Name: "local", DockerHostType: config.ContextLocal, ProjectDir: root}
}

func writeWorkbenchJobFixture(t *testing.T, root, name, contents string) string {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func readWorkbenchJobFixture(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parseWorkbenchJobFlags(t *testing.T, command *cobra.Command, args ...string) {
	t.Helper()
	if err := command.Flags().Parse(args); err != nil {
		t.Fatal(err)
	}
}

func assertNoWorkbenchJobSnapshots(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".sitectl-workbench-snapshot-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("Workbench snapshots were not cleaned up: %v", matches)
	}
}
