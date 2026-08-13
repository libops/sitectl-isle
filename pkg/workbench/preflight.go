package workbench

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// PathStatus describes whether Workbench can acquire a referenced media file.
type PathStatus string

const (
	// PathAvailable means the path is a readable regular file.
	PathAvailable PathStatus = "available"
	// PathMissing means the path or one of its ancestors does not exist.
	PathMissing PathStatus = "missing"
	// PathUnreadable means permissions or an access error prevent acquisition.
	PathUnreadable PathStatus = "unreadable"
	// PathInvalid means the path is outside policy, traverses a symlink, or is
	// not a regular file.
	PathInvalid PathStatus = "invalid"
)

// MediaReference identifies one file value in a Workbench input CSV.
type MediaReference struct {
	Row    int
	Column string
	Path   string
}

// PathInspection is the result of checking one Workbench media reference.
type PathInspection struct {
	Reference    MediaReference
	ResolvedPath string
	Status       PathStatus
	Err          error
}

// PathInspector supplies context-host metadata and an effective-user
// readability probe without coupling path policy to local or SSH transport.
type PathInspector interface {
	Lstat(name string) (fs.FileInfo, error)
	Readable(name string) error
}

// DefaultMediaColumns returns the file-bearing columns produced by Crosswalk
// and the retired Fabricator workflow.
func DefaultMediaColumns() []string {
	return []string{"file", "supplemental_file", "unpublished_supplemental_file"}
}

// ParseMediaReferences extracts non-HTTP file references from selected CSV
// columns. Pipe-separated Workbench values become individual references.
func ParseMediaReferences(r io.Reader, columns []string) ([]MediaReference, error) {
	wanted := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, fmt.Errorf("media column names must not be empty")
		}
		wanted[column] = struct{}{}
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("at least one media column is required")
	}

	file, err := readCSVWithHeader(r, "Workbench CSV", csvReadOptions{})
	if err != nil {
		return nil, err
	}
	type selectedColumn struct {
		index int
		name  string
	}
	indexes := make([]selectedColumn, 0, len(wanted))
	for index, name := range file.header {
		if _, ok := wanted[name]; ok {
			indexes = append(indexes, selectedColumn{index: index, name: name})
		}
	}
	if len(indexes) == 0 {
		available := make([]string, 0, len(wanted))
		for column := range wanted {
			available = append(available, column)
		}
		sort.Strings(available)
		return nil, fmt.Errorf("workbench CSV has none of the configured media columns: %s", strings.Join(available, ", "))
	}

	var references []MediaReference
	for {
		record, rowNumber, readErr := file.readRecord()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read Workbench CSV row %d: %w", rowNumber, readErr)
		}
		for _, column := range indexes {
			for _, value := range strings.Split(record[column.index], "|") {
				value = strings.TrimSpace(value)
				if value == "" || isHTTPMedia(value) {
					continue
				}
				references = append(references, MediaReference{Row: rowNumber, Column: column.name, Path: value})
			}
		}
	}
	return references, nil
}

func isHTTPMedia(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// InspectMediaReferences applies Workbench staging path normalization and
// classifies every reference without conflating missing and unreadable paths.
func InspectMediaReferences(references []MediaReference, stagingRoot string, allowedRoots []string, inspector PathInspector) ([]PathInspection, error) {
	if inspector == nil {
		return nil, fmt.Errorf("path inspector is required")
	}
	stagingRoot = path.Clean(strings.TrimSpace(stagingRoot))
	if !path.IsAbs(stagingRoot) {
		return nil, fmt.Errorf("staging root %q must be absolute", stagingRoot)
	}
	roots, err := normalizeRoots(allowedRoots)
	if err != nil {
		return nil, err
	}

	cache := make(map[string]PathInspection)
	results := make([]PathInspection, 0, len(references))
	for _, reference := range references {
		resolved, resolveErr := resolveMediaPath(reference.Path, stagingRoot)
		if resolveErr != nil {
			results = append(results, PathInspection{Reference: reference, ResolvedPath: reference.Path, Status: PathInvalid, Err: resolveErr})
			continue
		}
		cached, ok := cache[resolved]
		if !ok {
			cached = inspectPath(resolved, roots, inspector)
			cache[resolved] = cached
		}
		cached.Reference = reference
		results = append(results, cached)
	}
	return results, nil
}

func normalizeRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("at least one allowed media root is required")
	}
	result := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = path.Clean(strings.TrimSpace(root))
		if !path.IsAbs(root) || root == "/" {
			return nil, fmt.Errorf("allowed media root %q must be an absolute, non-root path", root)
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		result = append(result, root)
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result, nil
}

func resolveMediaPath(value, stagingRoot string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if looksLikeWindowsDrivePath(value) {
		return "", fmt.Errorf("windows drive paths are not supported on the Islandora context host")
	}
	if path.IsAbs(value) {
		return path.Clean(value), nil
	}
	resolved := path.Join(stagingRoot, value)
	if resolved != stagingRoot && !strings.HasPrefix(resolved, stagingRoot+"/") {
		return "", fmt.Errorf("relative media path escapes staging root %s", stagingRoot)
	}
	return resolved, nil
}

func looksLikeWindowsDrivePath(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}

func inspectPath(filename string, roots []string, inspector PathInspector) PathInspection {
	result := PathInspection{ResolvedPath: filename, Status: PathInvalid}
	root := matchingRoot(filename, roots)
	if root == "" {
		result.Err = fmt.Errorf("path is outside allowed media roots")
		return result
	}

	for _, current := range pathPrefixes(root, filename) {
		info, err := inspector.Lstat(current)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
				result.Status = PathMissing
				result.Err = fmt.Errorf("%s does not exist", current)
				return result
			}
			result.Status = PathUnreadable
			result.Err = fmt.Errorf("cannot inspect %s: %w", current, err)
			return result
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			result.Err = fmt.Errorf("path traverses symlink %s", current)
			return result
		}
		if current != filename {
			if !info.IsDir() {
				result.Err = fmt.Errorf("ancestor %s is not a directory", current)
				return result
			}
			if info.Mode().Perm()&0o111 == 0 {
				result.Status = PathUnreadable
				result.Err = fmt.Errorf("ancestor %s has no search permission", current)
				return result
			}
			continue
		}
		if !info.Mode().IsRegular() {
			result.Err = fmt.Errorf("path is not a regular file")
			return result
		}
		if info.Mode().Perm()&0o444 == 0 {
			result.Status = PathUnreadable
			result.Err = fmt.Errorf("file has no read permission")
			return result
		}
	}
	if err := inspector.Readable(filename); err != nil {
		result.Status = PathUnreadable
		result.Err = fmt.Errorf("effective context user cannot read file: %w", err)
		return result
	}
	result.Status = PathAvailable
	return result
}

func matchingRoot(filename string, roots []string) string {
	for _, root := range roots {
		if filename == root || strings.HasPrefix(filename, root+"/") {
			return root
		}
	}
	return ""
}

func pathPrefixes(root, filename string) []string {
	if root == filename {
		return []string{root}
	}
	result := []string{root}
	relative := strings.TrimPrefix(filename, root+"/")
	current := root
	for _, element := range strings.Split(relative, "/") {
		current = path.Join(current, element)
		result = append(result, current)
	}
	return result
}
