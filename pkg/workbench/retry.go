package workbench

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	uploadErrorPattern  = regexp.MustCompile(`^File not created for "([^"]+)", POST request to "/file/upload/media/(audio|document|file|image|video)/(field_media_audio_file|field_media_document|field_media_file|field_media_image|field_media_video_file)\?_format=json" returned an HTTP status code of "?504"?\b`)
	mediaErrorPattern   = regexp.MustCompile(`^Media not created, POST request to "/entity/media(?:\?_format=json)?" returned an HTTP status code of "?422"?\b`)
	payloadErrorPattern = regexp.MustCompile(`^JSON request body used in previous POST to "/entity/media(?:\?_format=json)?" was (.+)\.$`)
	finalErrorPattern   = regexp.MustCompile(`^Media for "?(.+?)"? not created(?: \(HTTP resp(?:onse|one) code 422\)|\. HTTP response code was 422)\.$`)
	nodeIDPattern       = regexp.MustCompile(`['"]field_media_of['"]\s*:\s*\[\s*\{[^}]*['"]target_id['"]\s*:\s*['"]?(\d+)['"]?`)
	mediaUsePattern     = regexp.MustCompile(`['"]field_media_use['"]\s*:\s*\[\s*\{[^}]*['"]target_id['"]\s*:\s*['"]?(\d+)['"]?`)
	publishedPattern    = regexp.MustCompile(`['"]status['"]\s*:\s*(?:\[\s*)?\{\s*['"]value['"]\s*:\s*['"]?([01])['"]?`)
)

// AddMediaRow is one idempotent Islandora Workbench add_media input row.
type AddMediaRow struct {
	NodeID      string
	File        string
	MediaUseTID string
	Published   string
}

// RetryRow is retained as the semantic name for a retry-planner result.
type RetryRow = AddMediaRow

type loggedError struct {
	line    int
	message string
}

// PlanMediaRetry parses a Workbench log and returns retry rows only when every
// logged error belongs to an allowlisted media-upload timeout cascade.
func PlanMediaRetry(r io.Reader) ([]RetryRow, error) {
	entries, err := collectLogEntries(r)
	if err != nil {
		return nil, err
	}
	errorsFound := make([]loggedError, 0, len(entries))
	for _, entry := range entries {
		switch entry.level {
		case "ERROR":
			errorsFound = append(errorsFound, loggedError{line: entry.line, message: entry.message})
		case "CRITICAL":
			return nil, fmt.Errorf("refusing retry: unexpected CRITICAL entry at Workbench log line %d", entry.line)
		}
	}
	if len(errorsFound) == 0 {
		return nil, fmt.Errorf("workbench log contains no ERROR entries to retry")
	}
	if len(errorsFound)%4 != 0 {
		return nil, fmt.Errorf("refusing retry: %d ERROR entries do not form complete four-entry media timeout cascades", len(errorsFound))
	}

	rows := make([]RetryRow, 0, len(errorsFound)/4)
	byNodeAndFile := make(map[string]RetryRow, len(errorsFound)/4)
	for i := 0; i < len(errorsFound); i += 4 {
		row, err := parseRetryCascade(errorsFound[i : i+4])
		if err != nil {
			return nil, err
		}
		key := row.NodeID + "\x00" + row.File
		if existing, ok := byNodeAndFile[key]; ok {
			if existing != row {
				return nil, fmt.Errorf("refusing retry: node %s file %q has conflicting media retry details", row.NodeID, row.File)
			}
			continue
		}
		byNodeAndFile[key] = row
		rows = append(rows, row)
	}
	return rows, nil
}

func parseRetryCascade(entries []loggedError) (RetryRow, error) {
	upload := uploadErrorPattern.FindStringSubmatch(entries[0].message)
	if len(upload) != 4 {
		return RetryRow{}, unexpectedRetryError(entries[0], "504 media-file upload failure")
	}
	if !mediaErrorPattern.MatchString(entries[1].message) {
		return RetryRow{}, unexpectedRetryError(entries[1], "422 media entity failure")
	}
	payload := payloadErrorPattern.FindStringSubmatch(entries[2].message)
	if len(payload) != 2 {
		return RetryRow{}, unexpectedRetryError(entries[2], "media entity request body")
	}
	final := finalErrorPattern.FindStringSubmatch(entries[3].message)
	if len(final) != 2 {
		return RetryRow{}, unexpectedRetryError(entries[3], "matching final 422 media failure")
	}
	if upload[1] != strings.TrimSpace(upload[1]) || final[1] != strings.TrimSpace(final[1]) || upload[1] != final[1] {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log lines %d and %d name different files (%q and %q)", entries[0].line, entries[3].line, upload[1], final[1])
	}
	filename := upload[1]
	// Retry logs are not bound to a trusted Crosswalk ArtifactPolicy. Keep
	// their file-read scope fixed to the default Workbench roots so log content
	// cannot authorize access elsewhere; custom-root deployments fail closed.
	if strings.ContainsAny(filename, "\x00\r\n") || !path.IsAbs(filename) || path.Clean(filename) != filename ||
		!strings.HasPrefix(filename, "/home/") && !strings.HasPrefix(filename, "/mnt/") {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log line %d file %q is not a canonical absolute path under the fixed retry roots /home or /mnt; standalone retries are not bound to a trusted artifact policy", entries[0].line, filename)
	}

	if !validMediaUploadEndpoint(upload[2], upload[3]) {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log line %d has mismatched media type %q and upload field %q", entries[0].line, upload[2], upload[3])
	}
	failedTargetPattern := regexp.MustCompile(`['"]` + regexp.QuoteMeta(upload[3]) + `['"]\s*:\s*\[\s*\{[^}]*['"]target_id['"]\s*:\s*False\b`)
	if uniqueCaptureWhole(payload[1], failedTargetPattern) == "" {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log line %d does not tie the 422 payload to failed upload field %q", entries[2].line, upload[3])
	}
	nodeID := uniqueCapture(payload[1], nodeIDPattern)
	mediaUseTID := uniqueCapture(payload[1], mediaUsePattern)
	published := uniqueCapture(payload[1], publishedPattern)
	if nodeID == "" || mediaUseTID == "" || published == "" {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log line %d does not contain numeric field_media_of, field_media_use, and status values", entries[2].line)
	}
	if nodeID == "0" || mediaUseTID == "0" {
		return RetryRow{}, fmt.Errorf("refusing retry: Workbench log line %d has a zero node or media-use ID", entries[2].line)
	}
	return RetryRow{
		NodeID:      nodeID,
		File:        filename,
		MediaUseTID: mediaUseTID,
		Published:   published,
	}, nil
}

func validMediaUploadEndpoint(mediaType, field string) bool {
	fields := map[string]string{
		"audio":    "field_media_audio_file",
		"document": "field_media_document",
		"file":     "field_media_file",
		"image":    "field_media_image",
		"video":    "field_media_video_file",
	}
	return fields[mediaType] == field
}

func uniqueCaptureWhole(value string, pattern *regexp.Regexp) string {
	matches := pattern.FindAllString(value, -1)
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func unexpectedRetryError(entry loggedError, expected string) error {
	return fmt.Errorf("refusing retry: unexpected ERROR at Workbench log line %d; expected %s", entry.line, expected)
}

func uniqueCapture(value string, pattern *regexp.Regexp) string {
	matches := pattern.FindAllStringSubmatch(value, -1)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return ""
	}
	return matches[0][1]
}

// WriteAddMediaCSV writes deterministic Workbench add_media input.
func WriteAddMediaCSV(w io.Writer, rows []AddMediaRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("at least one add_media row is required")
	}
	csvWriter := csv.NewWriter(w)
	if err := csvWriter.Write([]string{"node_id", "file", "media_use_tid", "published"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := csvWriter.Write([]string{row.NodeID, row.File, row.MediaUseTID, row.Published}); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

// WriteMediaRetryCSV writes deterministic Workbench add_media retry input.
func WriteMediaRetryCSV(w io.Writer, rows []RetryRow) error {
	return WriteAddMediaCSV(w, rows)
}

// ValidateMediaRetryLog requires one and only one Workbench success entry for
// every planned retry row, in addition to rejecting all ERROR entries.
func ValidateMediaRetryLog(r io.Reader, rows []RetryRow, siteURL string) error {
	entries, err := collectLogEntries(r)
	if err != nil {
		return err
	}
	if err := rejectLoggedErrors(entries); err != nil {
		return err
	}
	expectedSite, err := NormalizeSiteURL(siteURL)
	if err != nil {
		return err
	}
	expected := make(map[string]string, len(rows))
	successes := make(map[string]int, len(rows))
	for _, row := range rows {
		expected[row.NodeID+"\x00"+row.File] = row.NodeID
	}
	for _, entry := range entries {
		if entry.level != "INFO" {
			continue
		}
		const prefix = `Media for "`
		const middle = `" created and added to `
		if !strings.HasPrefix(entry.message, prefix) || !strings.HasSuffix(entry.message, ".") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(entry.message, prefix), ".")
		index := strings.Index(body, middle)
		if index < 0 {
			continue
		}
		filename := body[:index]
		target := body[index+len(middle):]
		marker := "/node/"
		nodeIndex := strings.LastIndex(target, marker)
		if nodeIndex < 1 {
			continue
		}
		nodeID := target[nodeIndex+len(marker):]
		key := nodeID + "\x00" + filename
		if _, ok := expected[key]; !ok {
			continue
		}
		normalized, normalizeErr := NormalizeSiteURL(target[:nodeIndex])
		if normalizeErr == nil && normalized == expectedSite {
			successes[key]++
		}
	}
	for _, row := range rows {
		count := successes[row.NodeID+"\x00"+row.File]
		if count != 1 {
			return fmt.Errorf("workbench log has %d verified success entries for node %s file %q, want exactly 1", count, row.NodeID, row.File)
		}
	}
	return nil
}

// ValidateRollbackLog requires one and only one Workbench node-deleted entry
// for every validated rollback node ID.
func ValidateRollbackLog(r io.Reader, nodeIDs []uint64, siteURL string) error {
	entries, err := collectLogEntries(r)
	if err != nil {
		return err
	}
	if err := rejectLoggedErrors(entries); err != nil {
		return err
	}
	expectedSite, err := NormalizeSiteURL(siteURL)
	if err != nil {
		return err
	}
	successes := make(map[uint64]int, len(nodeIDs))
	for _, entry := range entries {
		if entry.level != "INFO" || !strings.HasPrefix(entry.message, "Node ") || !strings.HasSuffix(entry.message, " deleted.") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(entry.message, "Node "), " deleted.")
		marker := "/node/"
		index := strings.LastIndex(value, marker)
		if index < 1 {
			continue
		}
		loggedSite, normalizeErr := NormalizeSiteURL(value[:index])
		if normalizeErr != nil || loggedSite != expectedSite {
			continue
		}
		id, parseErr := strconv.ParseUint(value[index+len(marker):], 10, 64)
		if parseErr == nil {
			successes[id]++
		}
	}
	for _, id := range nodeIDs {
		if successes[id] != 1 {
			return fmt.Errorf("workbench log has %d verified deletion entries for node %d, want exactly 1", successes[id], id)
		}
	}
	return nil
}

type logEntry struct {
	line    int
	level   string
	message string
}

var logEntryPattern = regexp.MustCompile(`(?i)\b(DEBUG|INFO|WARNING|ERROR|CRITICAL)\b\s*[-:]\s*(.+)$`)

func collectLogEntries(r io.Reader) ([]logEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var result []logEntry
	for line := 1; scanner.Scan(); line++ {
		matches := logEntryPattern.FindStringSubmatch(scanner.Text())
		if len(matches) == 3 {
			result = append(result, logEntry{line: line, level: strings.ToUpper(matches[1]), message: strings.TrimSpace(matches[2])})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Workbench log: %w", err)
	}
	return result, nil
}

func rejectLoggedErrors(entries []logEntry) error {
	count := 0
	first := 0
	for _, entry := range entries {
		if entry.level != "ERROR" && entry.level != "CRITICAL" {
			continue
		}
		count++
		if first == 0 {
			first = entry.line
		}
	}
	if count > 0 {
		return fmt.Errorf("workbench completed with %d ERROR/CRITICAL entries; first failure is at line %d", count, first)
	}
	return nil
}
