package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
)

func TestRunComponentReviewAppliesSelectedStates(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldApply := componentApplyOptions
	oldInput := componentReviewInput
	oldPromptState := componentReviewPromptState
	oldPromptChoice := componentReviewPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentApplyOptions = oldApply
		componentReviewInput = oldInput
		componentReviewPromptState = oldPromptState
		componentReviewPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs

	states := map[string]corecomponent.State{
		"fcrepo":            corecomponent.StateOff,
		"blazegraph":        corecomponent.StateOn,
		"isle-tls":          corecomponent.StateOn,
		"isle-tls-override": corecomponent.StateOn,
	}
	modes := map[string]string{
		"isle-tls-tls-mode":          traefikconfig.ModeLetsEncrypt,
		"isle-tls-override-tls-mode": traefikconfig.ModeHTTP,
	}

	var got createpkg.Options
	componentApplyOptions = func(opts createpkg.Options) error {
		got = opts
		return nil
	}
	componentReviewPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		return states[name], nil
	}
	componentReviewPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		return modes[name], nil
	}
	componentReviewInput = func(question ...string) (string, error) {
		return "y", nil
	}

	var out bytes.Buffer
	cmd := componentReviewCmd
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	if got.Path != projectDir {
		t.Fatalf("expected project path %q, got %q", projectDir, got.Path)
	}
	if got.Fcrepo != createpkg.FcrepoStateOff {
		t.Fatalf("expected fcrepo off, got %q", got.Fcrepo)
	}
	if got.Blazegraph != createpkg.FcrepoStateOn {
		t.Fatalf("expected blazegraph on, got %q", got.Blazegraph)
	}

	envText, err := os.ReadFile(filepath.Join(projectDir, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if !strings.Contains(string(envText), "URI_SCHEME=\"https\"") || !strings.Contains(string(envText), "TLS_PROVIDER=\"letsencrypt\"") {
		t.Fatalf("expected prod letsencrypt settings, got:\n%s", string(envText))
	}

	devOverride, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.dev.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.dev.yml) error = %v", err)
	}
	if !strings.Contains(string(devOverride), "DRUPAL_ENABLE_HTTPS: \"false\"") {
		t.Fatalf("expected dev http override, got:\n%s", string(devOverride))
	}

	rendered := out.String()
	if !strings.Contains(rendered, "isle-tls: on (letsencrypt)") {
		t.Fatalf("expected review output to include prod tls decision, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "isle-tls-override: on (http)") {
		t.Fatalf("expected review output to include dev tls decision, got:\n%s", rendered)
	}
}

func TestRunComponentReviewUsesDetectedTLSModeAsPromptDefault(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	if err := traefikconfig.ApplyProd(projectDir, traefikconfig.ModeLetsEncrypt); err != nil {
		t.Fatalf("ApplyProd() error = %v", err)
	}
	if err := traefikconfig.ApplyDev(projectDir, true, traefikconfig.ModeHTTP); err != nil {
		t.Fatalf("ApplyDev() error = %v", err)
	}

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldApply := componentApplyOptions
	oldInput := componentReviewInput
	oldPromptState := componentReviewPromptState
	oldPromptChoice := componentReviewPromptChoice
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentApplyOptions = oldApply
		componentReviewInput = oldInput
		componentReviewPromptState = oldPromptState
		componentReviewPromptChoice = oldPromptChoice
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentApplyOptions = func(opts createpkg.Options) error { return nil }
	componentReviewInput = func(question ...string) (string, error) { return "y", nil }

	var promptDefaults []string
	componentReviewPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		switch name {
		case "isle-tls", "isle-tls-override":
			return corecomponent.StateOn, nil
		default:
			return corecomponent.StateOn, nil
		}
	}
	componentReviewPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		promptDefaults = append(promptDefaults, name+"="+defaultValue)
		return defaultValue, nil
	}

	if err := runComponentReview(componentReviewCmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	if len(promptDefaults) != 2 {
		t.Fatalf("expected two tls mode prompts, got %v", promptDefaults)
	}
	if promptDefaults[0] != "isle-tls-tls-mode=letsencrypt" {
		t.Fatalf("expected prod default letsencrypt, got %v", promptDefaults)
	}
	if promptDefaults[1] != "isle-tls-override-tls-mode=http" {
		t.Fatalf("expected dev default http, got %v", promptDefaults)
	}
}

func TestBuildComponentReviewQuestionIncludesRuntimeTransitionWarnings(t *testing.T) {
	status := componentView{
		Definition: componentDefinitions()["blazegraph"],
		Name:       "blazegraph",
		State:      corecomponent.DetectedState(corecomponent.StateOff),
	}

	question := corecomponent.BuildReviewQuestion(status)

	if !strings.Contains(question, "If enabled: Existing content may need a triplestore backfill after enabling.") {
		t.Fatalf("expected enable warning in question, got:\n%s", question)
	}
	if !strings.Contains(question, "Impact: backfill likely required.") {
		t.Fatalf("expected backfill impact in question, got:\n%s", question)
	}
	if !strings.Contains(question, "If disabled: Disabling Blazegraph removes indexing integrations but does not require content migration.") {
		t.Fatalf("expected disable warning in question, got:\n%s", question)
	}
}

func TestRunComponentReviewReportDoesNotPromptOrApply(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldApply := componentApplyOptions
	oldPromptState := componentReviewPromptState
	oldPromptChoice := componentReviewPromptChoice
	oldReport := componentReviewReport
	oldVerbose := componentReviewVerbose
	oldFormat := componentReviewFormat
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentApplyOptions = oldApply
		componentReviewPromptState = oldPromptState
		componentReviewPromptChoice = oldPromptChoice
		componentReviewReport = oldReport
		componentReviewVerbose = oldVerbose
		componentReviewFormat = oldFormat
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentReviewReport = true
	componentReviewVerbose = false
	componentReviewFormat = corecomponent.ReportFormatSection

	componentApplyOptions = func(opts createpkg.Options) error {
		t.Fatal("expected report mode not to apply changes")
		return nil
	}
	componentReviewPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		t.Fatal("expected report mode not to prompt for state")
		return "", nil
	}
	componentReviewPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		t.Fatal("expected report mode not to prompt for extra choices")
		return "", nil
	}

	var out bytes.Buffer
	cmd := componentReviewCmd
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current state: `on`") {
		t.Fatalf("expected report output, got:\n%s", rendered)
	}
}

func TestRunComponentReviewReportTableFormat(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldReport := componentReviewReport
	oldVerbose := componentReviewVerbose
	oldFormat := componentReviewFormat
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentReviewReport = oldReport
		componentReviewVerbose = oldVerbose
		componentReviewFormat = oldFormat
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentReviewReport = true
	componentReviewVerbose = false
	componentReviewFormat = corecomponent.ReportFormatTable

	var out bytes.Buffer
	cmd := componentReviewCmd
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "COMPONENT") || !strings.Contains(rendered, "blazegraph") {
		t.Fatalf("expected table report output, got:\n%s", rendered)
	}
}
