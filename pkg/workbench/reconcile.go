package workbench

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SupplementalArtifact describes a Crosswalk supplemental-media artifact and
// the defaults to apply only when its optional cells are empty.
type SupplementalArtifact struct {
	Name               string
	Reader             io.Reader
	DefaultMediaUseTID string
	DefaultPublished   string
}

// ReconcileSupplementalMedia resolves create IDs to rollback node IDs and
// merges pending artifacts with existing Workbench add_media input.
func ReconcileSupplementalMedia(createCSV, rollbackCSV, existingAddMedia io.Reader, pending []SupplementalArtifact) ([]AddMediaRow, error) {
	if (createCSV == nil) != (rollbackCSV == nil) {
		return nil, fmt.Errorf("create and rollback CSV readers must be provided together")
	}
	idToNodeID := map[string]string{}
	if createCSV != nil {
		createFile, err := readNamedCSV(createCSV, "create CSV")
		if err != nil {
			return nil, err
		}
		if !createFile.columns["id"] {
			return nil, fmt.Errorf("create CSV is missing required id column")
		}
		nodeIDs, err := ParseRollbackCSV(rollbackCSV)
		if err != nil {
			return nil, err
		}
		if len(nodeIDs) != len(createFile.rows) {
			return nil, fmt.Errorf("refusing positional reconciliation: rollback CSV has %d node IDs but create CSV has %d rows", len(nodeIDs), len(createFile.rows))
		}

		idToNodeID = make(map[string]string, len(createFile.rows))
		for index, row := range createFile.rows {
			id := strings.TrimSpace(row.values["id"])
			if id == "" {
				continue
			}
			if _, duplicate := idToNodeID[id]; duplicate {
				return nil, fmt.Errorf("create CSV has duplicate id %q", id)
			}
			idToNodeID[id] = strconv.FormatUint(nodeIDs[index], 10)
		}
	}

	var merged []AddMediaRow
	if existingAddMedia != nil {
		rows, readErr := readAddMediaRows(existingAddMedia, "existing add_media CSV", "", "1", nil)
		if readErr != nil {
			return nil, readErr
		}
		merged = append(merged, rows...)
	}
	for index, artifact := range pending {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("pending supplemental artifact %d", index+1)
		}
		if artifact.Reader == nil {
			return nil, fmt.Errorf("%s reader is required", name)
		}
		rows, readErr := readAddMediaRows(artifact.Reader, name, artifact.DefaultMediaUseTID, artifact.DefaultPublished, idToNodeID)
		if readErr != nil {
			return nil, readErr
		}
		merged = append(merged, rows...)
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("supplemental reconciliation produced no add_media rows")
	}
	return deduplicateAddMediaRows(merged)
}

type namedCSVRow struct {
	number int
	values map[string]string
}

type namedCSV struct {
	columns map[string]bool
	rows    []namedCSVRow
}

func readNamedCSV(r io.Reader, name string) (namedCSV, error) {
	file, err := readCSVWithHeader(r, name, csvReadOptions{})
	if err != nil {
		return namedCSV{}, err
	}
	columns := make(map[string]bool, len(file.header))
	for _, column := range file.header {
		columns[column] = true
	}

	var rows []namedCSVRow
	for {
		record, rowNumber, readErr := file.readRecord()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return namedCSV{}, fmt.Errorf("read %s row %d: %w", name, rowNumber, readErr)
		}
		row := make(map[string]string, len(file.header))
		for column, index := range file.columnIndexes {
			row[column] = strings.TrimSpace(record[index])
		}
		rows = append(rows, namedCSVRow{number: rowNumber, values: row})
	}
	return namedCSV{columns: columns, rows: rows}, nil
}

func readAddMediaRows(r io.Reader, name, defaultMediaUseTID, defaultPublished string, idToNodeID map[string]string) ([]AddMediaRow, error) {
	file, err := readNamedCSV(r, name)
	if err != nil {
		return nil, err
	}
	if !file.columns["file"] {
		return nil, fmt.Errorf("%s is missing required file column", name)
	}
	if idToNodeID == nil && !file.columns["node_id"] {
		return nil, fmt.Errorf("%s is missing required node_id column", name)
	}
	if idToNodeID != nil && !file.columns["node_id"] && !file.columns["id"] {
		return nil, fmt.Errorf("%s must contain a node_id or id column", name)
	}
	result := make([]AddMediaRow, 0, len(file.rows))
	for _, inputRow := range file.rows {
		source := inputRow.values
		rowNumber := strconv.Itoa(inputRow.number)
		nodeID := source["node_id"]
		id := source["id"]
		mappedNodeID := idToNodeID[id]
		if nodeID == "" {
			if idToNodeID == nil {
				return nil, fmt.Errorf("%s row %s has no node_id", name, rowNumber)
			}
			if id == "" {
				return nil, fmt.Errorf("%s row %s has neither node_id nor id", name, rowNumber)
			}
			if mappedNodeID == "" {
				return nil, fmt.Errorf("%s row %s id %q is absent from create CSV", name, rowNumber, id)
			}
			nodeID = mappedNodeID
		} else if mappedNodeID != "" && mappedNodeID != nodeID {
			return nil, fmt.Errorf("%s row %s node_id %s conflicts with create id %q mapped to node %s", name, rowNumber, nodeID, id, mappedNodeID)
		}
		if _, parseErr := parsePositiveUint(nodeID); parseErr != nil {
			return nil, fmt.Errorf("%s row %s has invalid node_id %q", name, rowNumber, nodeID)
		}

		filename := source["file"]
		if filename == "" {
			return nil, fmt.Errorf("%s row %s has no file", name, rowNumber)
		}
		mediaUseTID := source["media_use_tid"]
		if mediaUseTID == "" {
			mediaUseTID = strings.TrimSpace(defaultMediaUseTID)
		}
		if mediaUseTID == "" && idToNodeID != nil {
			return nil, fmt.Errorf("%s row %s has no media_use_tid and no default was provided", name, rowNumber)
		}
		if mediaUseTID != "" {
			if _, parseErr := parsePositiveUint(mediaUseTID); parseErr != nil {
				return nil, fmt.Errorf("%s row %s has invalid media_use_tid %q", name, rowNumber, mediaUseTID)
			}
		}
		published := source["published"]
		if published == "" {
			published = strings.TrimSpace(defaultPublished)
		}
		if published != "0" && published != "1" {
			return nil, fmt.Errorf("%s row %s has published %q, want 0 or 1", name, rowNumber, published)
		}
		result = append(result, AddMediaRow{NodeID: nodeID, File: filename, MediaUseTID: mediaUseTID, Published: published})
	}
	return result, nil
}

func parsePositiveUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("not a positive integer")
	}
	return parsed, nil
}

func deduplicateAddMediaRows(rows []AddMediaRow) ([]AddMediaRow, error) {
	byNodeAndFile := make(map[string]AddMediaRow, len(rows))
	result := make([]AddMediaRow, 0, len(rows))
	for _, row := range rows {
		key := row.NodeID + "\x00" + row.File
		if existing, ok := byNodeAndFile[key]; ok {
			if existing != row {
				return nil, fmt.Errorf("node %s file %q has conflicting add_media rows", row.NodeID, row.File)
			}
			continue
		}
		byNodeAndFile[key] = row
		result = append(result, row)
	}
	return result, nil
}
