package workbench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseArtifactManifestAcceptsProducerV1Contract(t *testing.T) {
	t.Parallel()
	data := []byte("id,title\n1,Example\n")
	manifest := testArtifactManifest("target.csv", data)
	roots := []string{"/home", "/mnt"}
	manifest.Policy = &ArtifactPolicy{
		PathMode:             CrosswalkStagedPOSIXPathMode,
		StagingRoot:          "/mnt/islandora_staging",
		AllowedAbsoluteRoots: &roots,
	}
	manifest.ProfileFingerprint = strings.Repeat("b", 64)
	manifest.ModelFingerprint = strings.Repeat("c", 64)

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseArtifactManifest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Spec != manifest.Spec || parsed.ProfileFingerprint != manifest.ProfileFingerprint || parsed.ModelFingerprint != manifest.ModelFingerprint {
		t.Fatalf("parsed provenance = %#v", parsed)
	}
	if err := parsed.ValidateArtifact("target.csv", data); err != nil {
		t.Fatal(err)
	}
}

func TestParseArtifactManifestAcceptsKnownEmptyRootsAndUnavailablePolicy(t *testing.T) {
	t.Parallel()
	data := []byte("title\n")

	withoutPolicy := testArtifactManifest("target.csv", data)
	encoded, err := json.Marshal(withoutPolicy)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseArtifactManifest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("omitted policy rejected: %v", err)
	}
	if parsed.Policy != nil {
		t.Fatalf("omitted policy became %#v", parsed.Policy)
	}
	if err := parsed.ValidateArtifact("target.csv", data); err != nil {
		t.Fatalf("zero-row Crosswalk CSV rejected: %v", err)
	}

	emptyRoots := []string{}
	knownEmpty := testArtifactManifest("target.csv", data)
	knownEmpty.Policy = &ArtifactPolicy{
		PathMode: CrosswalkStagedPOSIXPathMode, StagingRoot: "/srv/workbench", AllowedAbsoluteRoots: &emptyRoots,
	}
	encoded, err = json.Marshal(knownEmpty)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = ParseArtifactManifest(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("known-empty allowed roots rejected: %v", err)
	}
	if parsed.Policy == nil || parsed.Policy.AllowedAbsoluteRoots == nil || len(*parsed.Policy.AllowedAbsoluteRoots) != 0 {
		t.Fatalf("known-empty allowed roots were not preserved: %#v", parsed.Policy)
	}
}

func TestArtifactManifestRequiresOperationPolicy(t *testing.T) {
	t.Parallel()
	pendingData := []byte("id,node_id,file,media_use_tid,published\na,,/mnt/item.pdf,,\n")
	unpublishedData := []byte("id,node_id,file,media_use_tid,published\na,,/mnt/private.pdf,,\n")

	tests := []struct {
		name     string
		path     string
		data     []byte
		policy   *ArtifactPolicy
		wantErr  string
		accepted bool
	}{
		{name: "pending pair", path: "target.pending_supplemental.csv", data: pendingData, policy: &ArtifactPolicy{SupplementalMediaUseTID: "16", PendingSupplementalPublished: "1"}, accepted: true},
		{name: "pending missing policy", path: "target.pending_supplemental.csv", data: pendingData, wantErr: "must define the supplemental"},
		{name: "pending wrong policy", path: "target.pending_supplemental.csv", data: pendingData, policy: &ArtifactPolicy{UnpublishedSupplementalMediaUseTID: "17", UnpublishedSupplementalPublished: "0"}, wantErr: "must define the supplemental"},
		{name: "unpublished pair", path: "target.unpublished_supplemental.csv", data: unpublishedData, policy: &ArtifactPolicy{UnpublishedSupplementalMediaUseTID: "17", UnpublishedSupplementalPublished: "0"}, accepted: true},
		{name: "unpublished missing policy", path: "target.unpublished_supplemental.csv", data: unpublishedData, wantErr: "must define the unpublished"},
		{name: "half pair", path: "target.csv", data: []byte("title\nExample\n"), policy: &ArtifactPolicy{SupplementalMediaUseTID: "16"}, wantErr: "must be provided together"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := testArtifactManifest(test.path, test.data)
			manifest.Policy = test.policy
			err := manifest.Validate()
			if test.accepted && err != nil {
				t.Fatal(err)
			}
			if !test.accepted && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestArtifactManifestRejectsUnsafePathsAndRoots(t *testing.T) {
	t.Parallel()
	data := []byte("title\nExample\n")
	for _, value := range []string{"../escape.csv", "nested/file.csv", "/absolute.csv", "a/../file.csv", `nested\file.csv`, CrosswalkArtifactManifestName} {
		value := value
		t.Run("path "+value, func(t *testing.T) {
			t.Parallel()
			manifest := testArtifactManifest("target.csv", data)
			manifest.Artifacts[0].Path = value
			if err := manifest.Validate(); err == nil {
				t.Fatalf("unsafe artifact path %q accepted", value)
			}
		})
	}

	rootTests := []struct {
		name    string
		staging string
		roots   []string
	}{
		{name: "root staging", staging: "/", roots: []string{"/mnt"}},
		{name: "relative staging", staging: "mnt/staging", roots: []string{"/mnt"}},
		{name: "unclean staging", staging: "/mnt/../staging", roots: []string{"/mnt"}},
		{name: "root allowlist", staging: "/mnt/staging", roots: []string{"/"}},
		{name: "outside allowlist", staging: "/srv/staging", roots: []string{"/mnt"}},
		{name: "duplicate allowlist", staging: "/mnt/staging", roots: []string{"/mnt", "/mnt"}},
	}
	for _, test := range rootTests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := testArtifactManifest("target.csv", data)
			manifest.Policy = &ArtifactPolicy{
				PathMode: CrosswalkStagedPOSIXPathMode, StagingRoot: test.staging, AllowedAbsoluteRoots: &test.roots,
			}
			if err := manifest.Validate(); err == nil {
				t.Fatalf("unsafe path policy accepted: %#v", manifest.Policy)
			}
		})
	}
}

func TestArtifactManifestRejectsInvalidVersionsAndProvenance(t *testing.T) {
	t.Parallel()
	data := []byte("title\nExample\n")
	tests := []struct {
		name   string
		mutate func(*ArtifactManifest)
	}{
		{name: "manifest version", mutate: func(m *ArtifactManifest) { m.Version++ }},
		{name: "spec version", mutate: func(m *ArtifactManifest) { m.Spec.Version = "2" }},
		{name: "spec digest uppercase", mutate: func(m *ArtifactManifest) { m.Spec.Fingerprint = strings.Repeat("A", 64) }},
		{name: "artifact digest nonhex", mutate: func(m *ArtifactManifest) { m.Artifacts[0].SHA256 = strings.Repeat("z", 64) }},
		{name: "profile without model", mutate: func(m *ArtifactManifest) { m.ProfileFingerprint = strings.Repeat("b", 64) }},
		{name: "model without profile", mutate: func(m *ArtifactManifest) { m.ModelFingerprint = strings.Repeat("c", 64) }},
		{name: "bad profile digest", mutate: func(m *ArtifactManifest) {
			m.ProfileFingerprint = "short"
			m.ModelFingerprint = strings.Repeat("c", 64)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := testArtifactManifest("target.csv", data)
			test.mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("invalid version or provenance accepted")
			}
		})
	}
}

func TestParseArtifactManifestRejectsAmbiguousJSON(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"unknown field":   `{"unknown":true}`,
		"duplicate field": `{"version":1,"version":1}`,
		"trailing value":  `{} {}`,
	} {
		name, input := name, input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseArtifactManifest(strings.NewReader(input)); err == nil {
				t.Fatalf("ambiguous JSON %q accepted", input)
			}
		})
	}
}

func TestArtifactManifestValidatesExactArtifactIntegrity(t *testing.T) {
	t.Parallel()
	data := []byte("title\nOriginal\n")
	manifest := testArtifactManifest("target.csv", data)

	if err := manifest.ValidateArtifact("target.csv", []byte("title\nTampered\n")); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	wrongBytes := manifest
	wrongBytes.Artifacts = append([]ArtifactDescriptor(nil), manifest.Artifacts...)
	wrongBytes.Artifacts[0].Bytes++
	if err := wrongBytes.ValidateArtifact("target.csv", data); err == nil || !strings.Contains(err.Error(), "byte count") {
		t.Fatalf("byte mismatch error = %v", err)
	}
	wrongRows := manifest
	wrongRows.Artifacts = append([]ArtifactDescriptor(nil), manifest.Artifacts...)
	wrongRows.Artifacts[0].CSVRows++
	if err := wrongRows.ValidateArtifact("target.csv", data); err == nil || !strings.Contains(err.Error(), "CSV row count") {
		t.Fatalf("row mismatch error = %v", err)
	}
	if err := manifest.ValidateArtifact("missing.csv", data); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("unlisted artifact error = %v", err)
	}
}

func testArtifactManifest(name string, data []byte) ArtifactManifest {
	digest := sha256.Sum256(data)
	rows := bytes.Count(data, []byte("\n")) - 1
	if rows < 0 {
		rows = 0
	}
	return ArtifactManifest{
		Version: CrosswalkArtifactManifestVersion,
		Spec: ArtifactSpec{
			Name: "Custom Workbench Profile", Version: CrosswalkSpecVersion, Fingerprint: strings.Repeat("a", 64),
		},
		Artifacts: []ArtifactDescriptor{{
			Path: name, MediaType: "text/csv; charset=utf-8", SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data)), CSVRows: rows,
		}},
	}
}
