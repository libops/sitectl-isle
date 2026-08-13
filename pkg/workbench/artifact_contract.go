package workbench

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

const (
	// CrosswalkArtifactContractName is the recommended filename for the
	// separately provisioned site trust anchor used with Crosswalk bundles.
	CrosswalkArtifactContractName = "crosswalk-workbench-contract.json"
	// CrosswalkArtifactContractVersion is the trust-anchor contract understood
	// by this release.
	CrosswalkArtifactContractVersion = 1

	maxCrosswalkArtifactContractBytes = int64(1 << 20)
)

// ArtifactContract is trusted site configuration installed independently from
// an uploaded Crosswalk batch. It contains no per-batch filenames or digests.
type ArtifactContract struct {
	Version            int             `json:"version"`
	Spec               ArtifactSpec    `json:"spec"`
	ProfileFingerprint string          `json:"profile_fingerprint,omitempty"`
	ModelFingerprint   string          `json:"model_fingerprint,omitempty"`
	Policy             *ArtifactPolicy `json:"policy,omitempty"`
}

// ParseArtifactContract decodes and validates a bounded, strict Crosswalk
// trust anchor. Unknown fields, duplicate names, and trailing values are
// rejected using the same JSON rules as artifact manifests.
func ParseArtifactContract(r io.Reader) (ArtifactContract, error) {
	var contract ArtifactContract
	if err := parseStrictArtifactJSON(r, maxCrosswalkArtifactContractBytes, "Crosswalk artifact contract", &contract); err != nil {
		return ArtifactContract{}, err
	}
	if err := contract.Validate(); err != nil {
		return ArtifactContract{}, err
	}
	return contract, nil
}

// Validate verifies the static trust-anchor schema independently from any
// uploaded batch.
func (c ArtifactContract) Validate() error {
	if c.Version != CrosswalkArtifactContractVersion {
		return fmt.Errorf("unsupported Crosswalk artifact contract version %d", c.Version)
	}
	if err := validateArtifactSpec(c.Spec, "contract"); err != nil {
		return err
	}
	if err := validateArtifactProvenance(c.ProfileFingerprint, c.ModelFingerprint, "contract"); err != nil {
		return err
	}
	if c.Policy != nil {
		if err := c.Policy.validate(); err != nil {
			return fmt.Errorf("crosswalk artifact contract policy: %w", err)
		}
	}
	return nil
}

// ValidateManifest requires every trusted specification, profile/model, and
// policy value to match the uploaded batch manifest. Allowed absolute roots
// are a set, so their order is not significant. Absence is significant: an
// unprofiled or policy-free contract cannot trust a manifest that self-declares
// those values.
func (c ArtifactContract) ValidateManifest(manifest ArtifactManifest) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	if c.Spec != manifest.Spec {
		return fmt.Errorf("crosswalk artifact manifest spec does not match the trusted site contract")
	}
	if c.ProfileFingerprint != manifest.ProfileFingerprint || c.ModelFingerprint != manifest.ModelFingerprint {
		return fmt.Errorf("crosswalk artifact manifest profile/model provenance does not match the trusted site contract")
	}
	if !artifactPoliciesEqual(c.Policy, manifest.Policy) {
		return fmt.Errorf("crosswalk artifact manifest policy does not match the trusted site contract")
	}
	return nil
}

func validateArtifactSpec(spec ArtifactSpec, source string) error {
	if spec.Name == "" || spec.Name != strings.TrimSpace(spec.Name) || len(spec.Name) > 512 || strings.ContainsAny(spec.Name, "\x00\r\n") {
		return fmt.Errorf("crosswalk artifact %s spec name is missing or invalid", source)
	}
	if spec.Version != CrosswalkSpecVersion {
		return fmt.Errorf("unsupported Crosswalk artifact %s spec version %q", source, spec.Version)
	}
	return validateManifestSHA256(source+" spec fingerprint", spec.Fingerprint)
}

func validateArtifactProvenance(profileFingerprint, modelFingerprint, source string) error {
	if (profileFingerprint == "") != (modelFingerprint == "") {
		return fmt.Errorf("crosswalk artifact %s profile_fingerprint and model_fingerprint must be provided together", source)
	}
	if profileFingerprint == "" {
		return nil
	}
	if err := validateManifestSHA256(source+" profile fingerprint", profileFingerprint); err != nil {
		return err
	}
	return validateManifestSHA256(source+" model fingerprint", modelFingerprint)
}

func artifactPoliciesEqual(left, right *ArtifactPolicy) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.PathMode != right.PathMode ||
		left.StagingRoot != right.StagingRoot ||
		left.SupplementalMediaUseTID != right.SupplementalMediaUseTID ||
		left.PendingSupplementalPublished != right.PendingSupplementalPublished ||
		left.UnpublishedSupplementalMediaUseTID != right.UnpublishedSupplementalMediaUseTID ||
		left.UnpublishedSupplementalPublished != right.UnpublishedSupplementalPublished {
		return false
	}
	if left.AllowedAbsoluteRoots == nil || right.AllowedAbsoluteRoots == nil {
		return left.AllowedAbsoluteRoots == right.AllowedAbsoluteRoots
	}
	if len(*left.AllowedAbsoluteRoots) != len(*right.AllowedAbsoluteRoots) {
		return false
	}
	leftRoots := slices.Clone(*left.AllowedAbsoluteRoots)
	rightRoots := slices.Clone(*right.AllowedAbsoluteRoots)
	slices.Sort(leftRoots)
	slices.Sort(rightRoots)
	return slices.Equal(leftRoots, rightRoots)
}
