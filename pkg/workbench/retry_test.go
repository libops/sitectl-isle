package workbench

import (
	"bytes"
	"strings"
	"testing"
)

const retryCascade = `2026-01-01 INFO starting
2026-01-01 ERROR - File not created for "/mnt/islandora_staging/item.tif", POST request to "/file/upload/media/file/field_media_file?_format=json" returned an HTTP status code of "504" and a response body of b''.
2026-01-01 ERROR - Media not created, POST request to "/entity/media" returned an HTTP status code of "422" and a response body of b'bad'.
2026-01-01 ERROR - JSON request body used in previous POST to "/entity/media" was {'field_media_file': [{'target_id': False}], 'field_media_of': [{'target_id': 436648}], 'field_media_use': [{'target_id': 16}], 'status': {'value': '1'}}.
2026-01-01 ERROR - Media for /mnt/islandora_staging/item.tif not created (HTTP respone code 422).
`

func TestPlanMediaRetryAcceptsOnlyCompleteCascadeAndDeduplicates(t *testing.T) {
	t.Parallel()
	rows, err := PlanMediaRetry(strings.NewReader(retryCascade + retryCascade))
	if err != nil {
		t.Fatal(err)
	}
	want := []RetryRow{{NodeID: "436648", File: "/mnt/islandora_staging/item.tif", MediaUseTID: "16", Published: "1"}}
	if len(rows) != len(want) || rows[0] != want[0] {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}

	var output bytes.Buffer
	if err := WriteMediaRetryCSV(&output, rows); err != nil {
		t.Fatal(err)
	}
	wantCSV := "node_id,file,media_use_tid,published\n436648,/mnt/islandora_staging/item.tif,16,1\n"
	if output.String() != wantCSV {
		t.Fatalf("CSV = %q, want %q", output.String(), wantCSV)
	}
}

func TestPlanMediaRetryRefusesUnrelatedError(t *testing.T) {
	t.Parallel()
	log := strings.Replace(retryCascade, "Media not created,", "Node not created,", 1)
	if _, err := PlanMediaRetry(strings.NewReader(log)); err == nil || !strings.Contains(err.Error(), "refusing retry") {
		t.Fatalf("error = %v, want fail-closed refusal", err)
	}
}

func TestPlanMediaRetryRefusesCriticalEntry(t *testing.T) {
	t.Parallel()
	if _, err := PlanMediaRetry(strings.NewReader(retryCascade + "2026 - CRITICAL - site unavailable\n")); err == nil || !strings.Contains(err.Error(), "CRITICAL") {
		t.Fatalf("critical error = %v", err)
	}
}

func TestPlanMediaRetryFixedRootsFailClosed(t *testing.T) {
	t.Parallel()
	log := strings.ReplaceAll(retryCascade, "/mnt/islandora_staging/item.tif", "/srv/workbench/item.tif")
	_, err := PlanMediaRetry(strings.NewReader(log))
	if err == nil || !strings.Contains(err.Error(), "fixed retry roots /home or /mnt") || !strings.Contains(err.Error(), "not bound to a trusted artifact policy") {
		t.Fatalf("custom-root error = %v", err)
	}
}

func TestPlanMediaRetryRefusesIncompleteAndMismatchedCascades(t *testing.T) {
	t.Parallel()
	lines := strings.Split(strings.TrimSpace(retryCascade), "\n")
	if _, err := PlanMediaRetry(strings.NewReader(strings.Join(lines[:len(lines)-1], "\n"))); err == nil || !strings.Contains(err.Error(), "complete four-entry") {
		t.Fatalf("incomplete error = %v", err)
	}

	mismatched := strings.Replace(retryCascade, "Media for /mnt/islandora_staging/item.tif", "Media for /mnt/islandora_staging/other.tif", 1)
	if _, err := PlanMediaRetry(strings.NewReader(mismatched)); err == nil || !strings.Contains(err.Error(), "different files") {
		t.Fatalf("mismatch error = %v", err)
	}
}

func TestPlanMediaRetryAcceptsWorkbenchMediaUploadVariants(t *testing.T) {
	t.Parallel()
	variants := []string{
		"/file/upload/media/audio/field_media_audio_file?_format=json",
		"/file/upload/media/document/field_media_document?_format=json",
		"/file/upload/media/image/field_media_image?_format=json",
		"/file/upload/media/video/field_media_video_file?_format=json",
	}
	for _, endpoint := range variants {
		parts := strings.Split(endpoint, "/")
		field := strings.TrimSuffix(parts[len(parts)-1], "?_format=json")
		log := strings.Replace(retryCascade, "/file/upload/media/file/field_media_file?_format=json", endpoint, 1)
		log = strings.Replace(log, "'field_media_file':", "'"+field+"':", 1)
		if _, err := PlanMediaRetry(strings.NewReader(log)); err != nil {
			t.Fatalf("endpoint %s rejected: %v", endpoint, err)
		}
	}
}

func TestPlanMediaRetryRefusesAmbiguousPayloadKeys(t *testing.T) {
	t.Parallel()
	ambiguous := strings.Replace(retryCascade, "'field_media_of':", "'field_media_of': [{'target_id': 999}], 'field_media_of':", 1)
	if _, err := PlanMediaRetry(strings.NewReader(ambiguous)); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("ambiguous payload error = %v", err)
	}
}

func TestValidateOperationLogsRequirePerTargetSuccess(t *testing.T) {
	t.Parallel()
	rows := []RetryRow{{NodeID: "42", File: "/mnt/item.tif", MediaUseTID: "16", Published: "1"}}
	mediaSuccess := `2026-01-01 - INFO - Media for "/mnt/item.tif" created and added to https://repo.example.org/islandora/node/42.` + "\n"
	if err := ValidateMediaRetryLog(strings.NewReader(mediaSuccess), rows, "https://repo.example.org/islandora/"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMediaRetryLog(strings.NewReader("2026 - WARNING - Node 42 not found.\n"), rows, "https://repo.example.org/islandora"); err == nil {
		t.Fatal("missing media success unexpectedly accepted")
	}

	rollbackSuccess := "2026-01-01 - INFO - Node https://repo.example.org/islandora/node/42 deleted.\n"
	if err := ValidateRollbackLog(strings.NewReader(rollbackSuccess), []uint64{42}, "https://repo.example.org/islandora"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRollbackLog(strings.NewReader("2026 - WARNING - Node 42 not found or not accessible, skipping delete.\n"), []uint64{42}, "https://repo.example.org/islandora"); err == nil {
		t.Fatal("missing node deletion unexpectedly accepted")
	}
}
