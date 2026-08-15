package workbench

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCrosswalkV1FixtureMatchesTrustedContract(t *testing.T) {
	t.Parallel()
	manifestData, err := os.ReadFile("testdata/crosswalk-artifacts-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseArtifactManifest(bytes.NewReader(manifestData))
	if err != nil {
		t.Fatal(err)
	}
	contractData, err := os.ReadFile("testdata/crosswalk-workbench-contract-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := ParseArtifactContract(bytes.NewReader(contractData))
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.ValidateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile("testdata/target.pending_supplemental.csv")
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateArtifact("target.pending_supplemental.csv", artifact); err != nil {
		t.Fatal(err)
	}
}

func TestParseArtifactContractAcceptsStrictV1TrustAnchor(t *testing.T) {
	t.Parallel()
	roots := []string{"/home", "/mnt"}
	contract := ArtifactContract{
		Version: CrosswalkArtifactContractVersion,
		Spec: ArtifactSpec{
			Name: "repository-items", Version: CrosswalkSpecVersion, Fingerprint: strings.Repeat("a", 64),
		},
		ProfileFingerprint: strings.Repeat("b", 64),
		ModelFingerprint:   strings.Repeat("c", 64),
		Policy: &ArtifactPolicy{
			PathMode: CrosswalkStagedPOSIXPathMode, StagingRoot: "/mnt/islandora_staging", AllowedAbsoluteRoots: &roots,
			SupplementalMediaUseTID: "16", PendingSupplementalPublished: "1",
		},
	}
	data, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseArtifactContract(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Spec != contract.Spec || !artifactPoliciesEqual(parsed.Policy, contract.Policy) {
		t.Fatalf("parsed contract = %#v", parsed)
	}
}

func TestArtifactContractRequiresPairedProvenanceAndStrictJSON(t *testing.T) {
	t.Parallel()
	contract := ArtifactContract{
		Version:            CrosswalkArtifactContractVersion,
		Spec:               ArtifactSpec{Name: "repository-items", Version: CrosswalkSpecVersion, Fingerprint: strings.Repeat("a", 64)},
		ProfileFingerprint: strings.Repeat("b", 64),
	}
	if err := contract.Validate(); err == nil || !strings.Contains(err.Error(), "must be provided together") {
		t.Fatalf("unpaired contract provenance error = %v", err)
	}
	for _, input := range []string{
		`{"version":1,"unknown":true}`,
		`{"version":1,"version":1}`,
		`{} {}`,
	} {
		if _, err := ParseArtifactContract(strings.NewReader(input)); err == nil {
			t.Fatalf("ambiguous contract JSON %q accepted", input)
		}
	}
}

func TestArtifactContractExactMatchesManifestTrustBoundary(t *testing.T) {
	t.Parallel()
	data := []byte("node_id,file\n42,/mnt/item.pdf\n")
	manifest := testArtifactManifest("target.csv", data)
	roots := []string{"/home", "/mnt"}
	manifest.ProfileFingerprint = strings.Repeat("b", 64)
	manifest.ModelFingerprint = strings.Repeat("c", 64)
	manifest.Policy = &ArtifactPolicy{
		PathMode: CrosswalkStagedPOSIXPathMode, StagingRoot: "/mnt/islandora_staging", AllowedAbsoluteRoots: &roots,
		SupplementalMediaUseTID: "16", PendingSupplementalPublished: "1",
	}
	trusted := ArtifactContract{
		Version: CrosswalkArtifactContractVersion,
		Spec:    manifest.Spec, ProfileFingerprint: manifest.ProfileFingerprint, ModelFingerprint: manifest.ModelFingerprint,
		Policy: cloneArtifactPolicy(manifest.Policy),
	}
	if err := trusted.ValidateManifest(manifest); err != nil {
		t.Fatalf("matching trusted contract rejected: %v", err)
	}
	reordered := trusted
	reordered.Policy = cloneArtifactPolicy(trusted.Policy)
	*reordered.Policy.AllowedAbsoluteRoots = []string{"/mnt", "/home"}
	if err := reordered.ValidateManifest(manifest); err != nil {
		t.Fatalf("equivalent reordered roots rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*ArtifactContract)
		wantErr string
	}{
		{name: "spec name", mutate: func(c *ArtifactContract) { c.Spec.Name = "other" }, wantErr: "spec does not match"},
		{name: "spec fingerprint", mutate: func(c *ArtifactContract) { c.Spec.Fingerprint = strings.Repeat("d", 64) }, wantErr: "spec does not match"},
		{name: "profile fingerprint", mutate: func(c *ArtifactContract) { c.ProfileFingerprint = strings.Repeat("d", 64) }, wantErr: "profile/model provenance"},
		{name: "model fingerprint", mutate: func(c *ArtifactContract) { c.ModelFingerprint = strings.Repeat("d", 64) }, wantErr: "profile/model provenance"},
		{name: "supplemental policy", mutate: func(c *ArtifactContract) { c.Policy.SupplementalMediaUseTID = "17" }, wantErr: "policy does not match"},
		{name: "path policy", mutate: func(c *ArtifactContract) { c.Policy.StagingRoot = "/mnt/other" }, wantErr: "policy does not match"},
		{name: "allowed root membership", mutate: func(c *ArtifactContract) { *c.Policy.AllowedAbsoluteRoots = []string{"/mnt", "/srv"} }, wantErr: "policy does not match"},
		{name: "policy omitted", mutate: func(c *ArtifactContract) { c.Policy = nil }, wantErr: "policy does not match"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contract := trusted
			contract.Policy = cloneArtifactPolicy(trusted.Policy)
			test.mutate(&contract)
			err := contract.ValidateManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateManifest() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func cloneArtifactPolicy(policy *ArtifactPolicy) *ArtifactPolicy {
	if policy == nil {
		return nil
	}
	clone := *policy
	if policy.AllowedAbsoluteRoots != nil {
		roots := append([]string(nil), (*policy.AllowedAbsoluteRoots)...)
		clone.AllowedAbsoluteRoots = &roots
	}
	return &clone
}
