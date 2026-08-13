package workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strconv"
	"strings"
)

const (
	// CrosswalkArtifactManifestName is the fixed manifest name in a Crosswalk
	// Workbench artifact directory.
	CrosswalkArtifactManifestName = "crosswalk-artifacts.json"
	// CrosswalkArtifactManifestVersion is the manifest contract understood by
	// this release.
	CrosswalkArtifactManifestVersion = 1
	// CrosswalkStagedPOSIXPathMode identifies Crosswalk's normalized Workbench
	// context-host path policy.
	CrosswalkStagedPOSIXPathMode = "staged-posix"
	// CrosswalkSpecVersion is the transformation-specification contract used by
	// version 1 manifests.
	CrosswalkSpecVersion = "1"

	maxCrosswalkArtifactManifestBytes = int64(4 << 20)
	maxCrosswalkArtifacts             = 64
	maxCrosswalkArtifactBytes         = int64(64 << 20)
	maxCrosswalkArtifactBundleBytes   = int64(256 << 20)
)

// ArtifactManifest binds one complete Crosswalk Workbench output set to the
// exact specification, optional site profile, and policy that produced it.
type ArtifactManifest struct {
	Version            int                  `json:"version"`
	Spec               ArtifactSpec         `json:"spec"`
	ProfileFingerprint string               `json:"profile_fingerprint,omitempty"`
	ModelFingerprint   string               `json:"model_fingerprint,omitempty"`
	Policy             *ArtifactPolicy      `json:"policy,omitempty"`
	Artifacts          []ArtifactDescriptor `json:"artifacts"`
}

// ArtifactSpec identifies the immutable Crosswalk transformation used to
// produce a Workbench artifact set.
type ArtifactSpec struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

// ArtifactPolicy records operational values selected by the Crosswalk
// transformation. sitectl validates and consumes these values but does not
// invent institution-specific defaults.
type ArtifactPolicy struct {
	PathMode                           string    `json:"path_mode,omitempty"`
	StagingRoot                        string    `json:"staging_root,omitempty"`
	AllowedAbsoluteRoots               *[]string `json:"allowed_absolute_roots,omitempty"`
	SupplementalMediaUseTID            string    `json:"supplemental_media_use_tid,omitempty"`
	PendingSupplementalPublished       string    `json:"pending_supplemental_published,omitempty"`
	UnpublishedSupplementalMediaUseTID string    `json:"unpublished_supplemental_media_use_tid,omitempty"`
	UnpublishedSupplementalPublished   string    `json:"unpublished_supplemental_published,omitempty"`
}

// ArtifactDescriptor records the exact bytes and CSV data-row count of one
// normalized relative file in a Crosswalk Workbench artifact directory.
type ArtifactDescriptor struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	CSVRows   int    `json:"csv_rows"`
}

// ParseArtifactManifest decodes and validates one bounded, strict Crosswalk
// Workbench artifact manifest. Unknown fields, duplicate JSON names, and
// trailing JSON values are rejected so producer and consumer cannot silently
// disagree about policy.
func ParseArtifactManifest(r io.Reader) (ArtifactManifest, error) {
	var manifest ArtifactManifest
	if err := parseStrictArtifactJSON(r, maxCrosswalkArtifactManifestBytes, "Crosswalk artifact manifest", &manifest); err != nil {
		return ArtifactManifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return ArtifactManifest{}, err
	}
	return manifest, nil
}

func parseStrictArtifactJSON(r io.Reader, maxBytes int64, label string, target any) error {
	if r == nil {
		return fmt.Errorf("%s reader is required", label)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("%s is empty", label)
	}
	if err := rejectDuplicateJSONNames(data); err != nil {
		return fmt.Errorf("parse %s: %w", label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse %s: %w", label, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("parse %s: trailing JSON value", label)
		}
		return fmt.Errorf("parse %s: %w", label, err)
	}
	return nil
}

// Validate verifies the static manifest contract independently from artifact
// transport. Artifact bytes are verified separately with ValidateArtifact.
func (m ArtifactManifest) Validate() error {
	if m.Version != CrosswalkArtifactManifestVersion {
		return fmt.Errorf("unsupported Crosswalk artifact manifest version %d", m.Version)
	}
	if err := validateArtifactSpec(m.Spec, "manifest"); err != nil {
		return err
	}
	if err := validateArtifactProvenance(m.ProfileFingerprint, m.ModelFingerprint, "manifest"); err != nil {
		return err
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("crosswalk artifact manifest contains no artifacts")
	}
	if len(m.Artifacts) > maxCrosswalkArtifacts {
		return fmt.Errorf("crosswalk artifact manifest contains %d artifacts, exceeds %d-artifact limit", len(m.Artifacts), maxCrosswalkArtifacts)
	}

	seen := make(map[string]struct{}, len(m.Artifacts))
	var totalBytes int64
	for index, artifact := range m.Artifacts {
		if err := artifact.validate(); err != nil {
			return fmt.Errorf("crosswalk artifact manifest artifact %d: %w", index+1, err)
		}
		if _, duplicate := seen[artifact.Path]; duplicate {
			return fmt.Errorf("crosswalk artifact manifest contains duplicate path %q", artifact.Path)
		}
		seen[artifact.Path] = struct{}{}
		totalBytes += artifact.Bytes
		if totalBytes > maxCrosswalkArtifactBundleBytes {
			return fmt.Errorf("crosswalk artifact manifest declares %d bytes, exceeds %d-byte bundle limit", totalBytes, maxCrosswalkArtifactBundleBytes)
		}
	}
	if m.Policy != nil {
		if err := m.Policy.validate(); err != nil {
			return fmt.Errorf("crosswalk artifact manifest policy: %w", err)
		}
	}
	if _, present := seen["target.pending_supplemental.csv"]; present {
		if m.Policy == nil || m.Policy.SupplementalMediaUseTID == "" || m.Policy.PendingSupplementalPublished == "" {
			return fmt.Errorf("crosswalk artifact manifest policy must define the supplemental media-use and publication pair for target.pending_supplemental.csv")
		}
	}
	if _, present := seen["target.unpublished_supplemental.csv"]; present {
		if m.Policy == nil || m.Policy.UnpublishedSupplementalMediaUseTID == "" || m.Policy.UnpublishedSupplementalPublished == "" {
			return fmt.Errorf("crosswalk artifact manifest policy must define the unpublished supplemental media-use and publication pair for target.unpublished_supplemental.csv")
		}
	}
	return nil
}

// Artifact returns the descriptor for one exact normalized manifest path.
func (m ArtifactManifest) Artifact(name string) (ArtifactDescriptor, bool) {
	for _, artifact := range m.Artifacts {
		if artifact.Path == name {
			return artifact, true
		}
	}
	return ArtifactDescriptor{}, false
}

// ValidateArtifact verifies exact byte length, SHA-256, valid CSV structure,
// and data-row count for one artifact listed in the manifest.
func (m ArtifactManifest) ValidateArtifact(name string, data []byte) error {
	artifact, ok := m.Artifact(name)
	if !ok {
		return fmt.Errorf("crosswalk artifact %q is not listed in %s", name, CrosswalkArtifactManifestName)
	}
	if int64(len(data)) != artifact.Bytes {
		return fmt.Errorf("crosswalk artifact %q byte count is %d, want %d", name, len(data), artifact.Bytes)
	}
	digest := sha256.Sum256(data)
	actualDigest := hex.EncodeToString(digest[:])
	if actualDigest != artifact.SHA256 {
		return fmt.Errorf("crosswalk artifact %q SHA-256 mismatch: got %s, want %s", name, actualDigest, artifact.SHA256)
	}
	rows, err := countArtifactCSVRows(data)
	if err != nil {
		return fmt.Errorf("crosswalk artifact %q is not valid CSV: %w", name, err)
	}
	if rows != artifact.CSVRows {
		return fmt.Errorf("crosswalk artifact %q CSV row count is %d, want %d", name, rows, artifact.CSVRows)
	}
	return nil
}

func (p ArtifactPolicy) validate() error {
	hasPathMode := p.PathMode != ""
	hasStagingRoot := p.StagingRoot != ""
	hasAllowedRoots := p.AllowedAbsoluteRoots != nil
	if hasPathMode || hasStagingRoot || hasAllowedRoots {
		if !hasPathMode || !hasStagingRoot || !hasAllowedRoots {
			return fmt.Errorf("path_mode, staging_root, and allowed_absolute_roots must be provided together")
		}
		if p.PathMode != CrosswalkStagedPOSIXPathMode {
			return fmt.Errorf("unsupported path_mode %q", p.PathMode)
		}
		if err := validateManifestRoot("staging_root", p.StagingRoot); err != nil {
			return err
		}
		if len(*p.AllowedAbsoluteRoots) > 128 {
			return fmt.Errorf("allowed_absolute_roots contains %d entries, exceeds 128-entry limit", len(*p.AllowedAbsoluteRoots))
		}
		seen := make(map[string]struct{}, len(*p.AllowedAbsoluteRoots))
		stagingAllowed := false
		for index, root := range *p.AllowedAbsoluteRoots {
			if err := validateManifestRoot(fmt.Sprintf("allowed_absolute_roots[%d]", index), root); err != nil {
				return err
			}
			if _, duplicate := seen[root]; duplicate {
				return fmt.Errorf("allowed absolute root %q is duplicated", root)
			}
			seen[root] = struct{}{}
			if pathWithinManifestRoot(p.StagingRoot, root) {
				stagingAllowed = true
			}
		}
		if len(*p.AllowedAbsoluteRoots) > 0 && !stagingAllowed {
			return fmt.Errorf("staging_root %q is outside allowed_absolute_roots", p.StagingRoot)
		}
	}
	if err := validateManifestPolicyPair("supplemental", p.SupplementalMediaUseTID, p.PendingSupplementalPublished); err != nil {
		return err
	}
	if err := validateManifestPolicyPair("unpublished supplemental", p.UnpublishedSupplementalMediaUseTID, p.UnpublishedSupplementalPublished); err != nil {
		return err
	}
	return nil
}

func (a ArtifactDescriptor) validate() error {
	if err := validateManifestRelativePath(a.Path); err != nil {
		return err
	}
	if a.Path == CrosswalkArtifactManifestName {
		return fmt.Errorf("path must not name the manifest itself")
	}
	mediaType, parameters, err := mime.ParseMediaType(a.MediaType)
	if err != nil || mediaType != "text/csv" || len(parameters) > 1 || (len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8")) {
		return fmt.Errorf("artifact %q media_type must identify UTF-8 CSV", a.Path)
	}
	if a.MediaType != "text/csv" && a.MediaType != "text/csv; charset=utf-8" {
		return fmt.Errorf("artifact %q media_type %q is not normalized", a.Path, a.MediaType)
	}
	if err := validateManifestSHA256(fmt.Sprintf("artifact %q SHA-256", a.Path), a.SHA256); err != nil {
		return err
	}
	if a.Bytes <= 0 {
		return fmt.Errorf("artifact %q bytes must be positive", a.Path)
	}
	if a.Bytes > maxCrosswalkArtifactBytes {
		return fmt.Errorf("artifact %q bytes %d exceeds %d-byte limit", a.Path, a.Bytes, maxCrosswalkArtifactBytes)
	}
	if a.CSVRows < 0 {
		return fmt.Errorf("artifact %q csv_rows must not be negative", a.Path)
	}
	return nil
}

func validateManifestRelativePath(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 1024 {
		return fmt.Errorf("artifact path %q is not a normalized relative path", value)
	}
	if strings.ContainsAny(value, "\\\x00\r\n") || path.IsAbs(value) || value == "." || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("artifact path %q is not a normalized relative path", value)
	}
	if path.Base(value) != value {
		return fmt.Errorf("artifact path %q must be a filename without directories", value)
	}
	return nil
}

func validateManifestSHA256(name, value string) error {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return fmt.Errorf("crosswalk artifact manifest %s must be 64 lowercase hexadecimal characters", name)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("crosswalk artifact manifest %s must be 64 lowercase hexadecimal characters", name)
	}
	return nil
}

func validateManifestPolicyPair(name, mediaUseTID, published string) error {
	hasTID := mediaUseTID != ""
	hasPublished := published != ""
	if hasTID != hasPublished {
		return fmt.Errorf("%s media-use and publication values must be provided together", name)
	}
	if !hasTID {
		return nil
	}
	parsed, err := strconv.ParseUint(mediaUseTID, 10, 64)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != mediaUseTID {
		return fmt.Errorf("%s media-use term ID %q must be a canonical positive integer", name, mediaUseTID)
	}
	if published != "0" && published != "1" {
		return fmt.Errorf("%s published value %q must be 0 or 1", name, published)
	}
	return nil
}

func validateManifestRoot(name, value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 4096 || strings.ContainsAny(value, "\\\x00\r\n") || !path.IsAbs(value) || path.Clean(value) != value || value == "/" {
		return fmt.Errorf("%s %q must be a clean absolute non-root POSIX path", name, value)
	}
	return nil
}

func pathWithinManifestRoot(value, root string) bool {
	return value == root || strings.HasPrefix(value, root+"/")
}

func countArtifactCSVRows(data []byte) (int, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return 0, fmt.Errorf("read header: %w", err)
	}
	if len(header) == 0 {
		return 0, fmt.Errorf("header is empty")
	}
	seen := make(map[string]struct{}, len(header))
	for index, raw := range header {
		name := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if name == "" {
			return 0, fmt.Errorf("header column %d is empty", index+1)
		}
		if _, duplicate := seen[name]; duplicate {
			return 0, fmt.Errorf("header column %q is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	rows := 0
	for {
		_, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
		rows++
	}
	return rows, nil
}

func rejectDuplicateJSONNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting exceeds 64-level limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object name is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON name %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("JSON array is not closed")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}
