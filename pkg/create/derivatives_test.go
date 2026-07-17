package create

import "testing"

func TestDerivativeServiceCatalogMatchesISLETemplateImages(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"fits":      "libops/fits@sha256:f945c3aa9b1011261a038d6a2a1ddc7557d1d0c59e00add9a82a47334e78b76f",
		"crayfits":  "libops/crayfits:main@sha256:9939b6288328e42ba8f82e79a99dd85ca4a0bb0d0f52b29466b66eaa4504e9cb",
		"homarus":   "libops/homarus:8.1.2@sha256:dede2629a054497e8220f5cd4602f2f5d5bc249a430cb12ece61f8ef15819e9a",
		"houdini":   "libops/houdini:8.18.2@sha256:347005f6ba008b48e0f0e5b10d5cd30582883dab9bd514dae72ff59bd5eac8e2",
		"hypercube": "libops/hypercube:5.5.2@sha256:d9ebaf350cb09ae814ae4fb2b4f38182c6f04032b05fff4cb8c2ad24f8006e9a",
	}
	for _, spec := range DerivativeServiceSpecs() {
		if spec.ImageRef != want[spec.Name] {
			t.Fatalf("%s image = %q, want %q", spec.Name, spec.ImageRef, want[spec.Name])
		}
		delete(want, spec.Name)
	}
	if len(want) != 0 {
		t.Fatalf("catalog missing expected services: %#v", want)
	}
}

func TestMergePDFIsManagedAsAFeatureBundle(t *testing.T) {
	t.Parallel()
	if IsDerivativeService(FeatureBundleMergePDF) {
		t.Fatal("mergepdf must not be registered as a service-only derivative component")
	}
	if !IsFeatureBundle(FeatureBundleMergePDF) {
		t.Fatal("mergepdf feature bundle is not registered")
	}
}
