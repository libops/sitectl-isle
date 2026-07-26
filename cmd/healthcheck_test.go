package cmd

import "testing"

func TestIsleDrupalHealthcheckURLUsesStableDrupalRoute(t *testing.T) {
	t.Parallel()

	got := isleDrupalHealthcheckURL("https://repo.example.org:8443/old?query=value#fragment")
	want := "https://repo.example.org:8443/user/login"
	if got != want {
		t.Fatalf("isleDrupalHealthcheckURL() = %q, want %q", got, want)
	}
}

func TestIsleDrupalHealthcheckURLPreservesInvalidTargetForDiagnostics(t *testing.T) {
	t.Parallel()

	const target = "not a URL"
	if got := isleDrupalHealthcheckURL(target); got != target {
		t.Fatalf("isleDrupalHealthcheckURL() = %q, want %q", got, target)
	}
}
