package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl-isle/pkg/components"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	"github.com/spf13/cobra"
)

func newComponentReviewTestCommand() *cobra.Command {
	var path string
	var codebaseRootfs string
	var drupalRootfs string
	var componentName string
	var report bool
	var verbose bool
	var format string
	var yolo bool

	cmd := &cobra.Command{Use: "review"}
	cmd.Flags().StringVar(&path, "path", "", "Path to the checked out ISLE project. Defaults to the active sitectl context project directory")
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().StringVarP(&componentName, "component", "c", "", "Specific component to reconcile")
	corecomponent.AddReviewFlags(cmd, &report, &verbose, &format)
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Apply without confirmation")
	return cmd
}

func TestRunComponentReviewAppliesSelectedStates(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldDrupalRootfs := statusDrupalRootfs
	oldApply := componentApplyOptions
	oldInput := componentReviewInput
	oldPromptState := componentReviewPromptState
	oldPromptChoice := componentReviewPromptChoice
	oldYolo := componentReviewYolo
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldDrupalRootfs
		componentApplyOptions = oldApply
		componentReviewInput = oldInput
		componentReviewPromptState = oldPromptState
		componentReviewPromptChoice = oldPromptChoice
		componentReviewYolo = oldYolo
	})

	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	componentReviewYolo = true

	states := map[string]corecomponent.State{
		"fcrepo":        corecomponent.StateOff,
		"blazegraph":    corecomponent.StateOn,
		"iiif":          corecomponent.StateOff,
		"iiif-topology": corecomponent.StateOn,
		"dev-mode":      corecomponent.StateOff,
		"ingress":       corecomponent.StateOn,
	}
	modes := map[string]string{
		"isle-file-system-uri": createpkg.PrivateISLEFileSystemURI,
		"mode":                 coretraefik.IngressModeHTTPSLetsEncrypt,
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
	inputCalls := 0
	componentReviewInput = func(question ...string) (string, error) {
		inputCalls++
		joined := strings.ToLower(strings.Join(question, "\n"))
		switch {
		case strings.Contains(joined, "continue?"):
			return "y", nil
		case strings.Contains(joined, "trusted proxy"):
			return "", nil
		case strings.Contains(joined, "upstream"):
			return "http://cantaloupe.example:8182", nil
		case strings.Contains(joined, "domain"):
			return "repo.example.org", nil
		case strings.Contains(joined, "acme"):
			return "admin@example.org", nil
		case strings.Contains(joined, "max upload"), strings.Contains(joined, "upload/read timeout"):
			return "", nil
		default:
			return "y", nil
		}
	}

	var out bytes.Buffer
	cmd := newComponentReviewTestCommand()
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
	if got.IIIF != createpkg.IIIFCantaloupe {
		t.Fatalf("expected iiif cantaloupe, got %q", got.IIIF)
	}
	if got.IIIFTopology != createpkg.IIIFTopologyExternal {
		t.Fatalf("expected distributed iiif topology, got %q", got.IIIFTopology)
	}
	if got.IIIFUpstreamURL != "http://cantaloupe.example:8182" {
		t.Fatalf("expected iiif upstream url, got %q", got.IIIFUpstreamURL)
	}

	composeText, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	if !strings.Contains(string(composeText), `INGRESS_HOSTNAMES: "repo.example.org,localhost,127.0.0.1,::1"`) ||
		!strings.Contains(string(composeText), `INGRESS_SCHEME: "https"`) ||
		!strings.Contains(string(composeText), "--certificatesResolvers.letsencrypt.acme.email=admin@example.org") {
		t.Fatalf("expected prod letsencrypt settings, got:\n%s", string(composeText))
	}
	routerText, err := os.ReadFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"))
	if err != nil {
		t.Fatalf("ReadFile(drupal router) error = %v", err)
	}
	if !strings.Contains(string(routerText), "certResolver: letsencrypt") {
		t.Fatalf("expected prod letsencrypt router, got:\n%s", string(routerText))
	}

	rendered := out.String()
	if !strings.Contains(rendered, "iiif-topology: distributed (http://cantaloupe.example:8182)") {
		t.Fatalf("expected review output to include distributed iiif decision, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "ingress: enabled (https-letsencrypt)") {
		t.Fatalf("expected review output to include ingress decision, got:\n%s", rendered)
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

func TestDriftedComponentViewsPreservesOnlyDrift(t *testing.T) {
	t.Parallel()
	views := []componentView{
		{Name: "enabled", State: corecomponent.DetectedState(corecomponent.StateOn)},
		{Name: "disabled", State: corecomponent.DetectedState(corecomponent.StateOff)},
		{Name: "drifted", State: corecomponent.StateDrifted},
	}
	got := driftedComponentViews(views)
	if len(got) != 1 || got[0].Name != "drifted" {
		t.Fatalf("driftedComponentViews() = %+v, want only drifted", got)
	}
}

func TestReconciliationDefinitionsUseLocalDerivativeRulesForEnabledDisposition(t *testing.T) {
	t.Parallel()
	definition := components.DerivativeService("crayfits")
	desired := corecomponent.NewDesiredState("isle")
	if err := desired.Set(definition, corecomponent.DispositionEnabled, nil); err != nil {
		t.Fatal(err)
	}

	adapted := reconciliationDefinitionsForDesiredState([]corecomponent.Definition{definition}, desired)
	if len(adapted) != 1 {
		t.Fatalf("adapted definitions = %d, want 1", len(adapted))
	}
	if got := adapted[0].On.Compose.Rules[0].Op; got != corecomponent.OpRestore {
		t.Fatalf("enabled derivative rule = %q, want restore local service", got)
	}
	if got := definition.On.Compose.Rules[0].Op; got != corecomponent.OpDelete {
		t.Fatalf("source definition was mutated: rule = %q, want delete", got)
	}
}

func TestRunComponentReconcileDesiredStatePreservesHealthyTopology(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeISLEDefaultCodebaseFixture(t, projectDir)
	for _, service := range createpkg.DerivativeServiceNames() {
		addDerivativeServiceFixture(t, projectDir, service)
	}

	ctx, err := localStatusContext(projectDir)
	if err != nil {
		t.Fatalf("localStatusContext() error = %v", err)
	}
	if err := applyIngressReviewDecision(ctx, componentReviewDecision{
		State:         corecomponent.StateOn,
		TLSMode:       coretraefik.IngressModeHTTP,
		Domain:        coretraefik.DefaultIngressDomain,
		MaxUploadSize: coretraefik.DefaultMaxUploadSize,
		UploadTimeout: coretraefik.DefaultUploadTimeout,
	}); err != nil {
		t.Fatalf("applyIngressReviewDecision() error = %v", err)
	}
	// Keep ingress healthy under the currently released core detector, which
	// evaluates each supported Compose filename independently. The v0.37 core
	// candidate-file behavior collapses these to the files that actually exist.
	composeFixture := readFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"))
	for _, name := range []string{"docker-compose.yaml", "compose.yml", "compose.yaml"} {
		writeFileForTest(t, filepath.Join(projectDir, name), composeFixture)
	}

	composePath := filepath.Join(projectDir, "docker-compose.yml")
	compose := readFileForTest(t, composePath)
	compose = strings.Replace(compose, "\n  traefik:\n", "\n  triplet:\n    image: libops/triplet:test\n\n  traefik:\n", 1)
	writeFileForTest(t, composePath, compose)

	before, err := detectComponentViewsForContext(ctx, createpkg.DefaultDrupalRootfs)
	if err != nil {
		t.Fatalf("detectComponentViewsForContext(before) error = %v", err)
	}
	beforeByName := componentViewsByName(before)
	drifted := driftedComponentViews(before)
	if len(drifted) != 1 || drifted[0].Name != "iiif" {
		details := make([]string, 0, len(drifted))
		for _, view := range drifted {
			details = append(details, view.Name+": "+componentDriftSummary(view, 10))
		}
		t.Fatalf("fixture drift = %v, want only iiif", details)
	}

	oldApply := componentApplyOptions
	oldInput := componentReviewInput
	oldPromptState := componentReviewPromptState
	oldPromptDisposition := componentReviewPromptDisposition
	oldPromptChoice := componentReviewPromptChoice
	t.Cleanup(func() {
		componentApplyOptions = oldApply
		componentReviewInput = oldInput
		componentReviewPromptState = oldPromptState
		componentReviewPromptDisposition = oldPromptDisposition
		componentReviewPromptChoice = oldPromptChoice
	})

	var got createpkg.Options
	componentApplyOptions = func(opts createpkg.Options) error {
		got = opts
		return createpkg.Apply(opts)
	}
	componentReviewPromptDisposition = nil
	componentReviewPromptState = func(name string, guidance corecomponent.StateGuidance, input corecomponent.InputFunc) (corecomponent.State, error) {
		t.Fatalf("unexpected desired-state prompt for %q", name)
		return "", nil
	}
	componentReviewPromptChoice = func(name string, choices []corecomponent.Choice, defaultValue string, input corecomponent.InputFunc, sections ...string) (string, error) {
		t.Fatalf("unexpected follow-up prompt for %q", name)
		return "", nil
	}
	componentReviewInput = func(question ...string) (string, error) {
		t.Fatalf("unexpected text prompt: %s", strings.Join(question, "\n"))
		return "", nil
	}
	testContext := &config.Context{Plugin: "isle", DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	views, err := detectComponentViewsForContext(testContext, createpkg.DefaultDrupalRootfs)
	if err != nil {
		t.Fatal(err)
	}
	desiredDecisions := make(map[string]corecomponent.ReviewDecision, len(views))
	for _, view := range views {
		desiredDecisions[view.Name] = corecomponent.ReviewDecision{Disposition: view.Disposition, Options: view.FollowUpValues}
	}
	desiredDecisions["iiif"] = corecomponent.ReviewDecision{Disposition: corecomponent.DispositionCantaloupe}
	desired, err := corecomponent.DesiredStateFromDecisions("isle", orderedComponentDefinitions(), desiredDecisions)
	if err != nil {
		t.Fatal(err)
	}
	if err := corecomponent.SaveDesiredState(testContext, desired); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd := newComponentReviewTestCommand()
	cmd.SetOut(&out)
	if err := runComponentReconcile(cmd, componentReconcileOptions{
		Path:         projectDir,
		DrupalRootfs: createpkg.DefaultDrupalRootfs,
		Yolo:         true,
		DriftOnly:    true,
	}); err != nil {
		t.Fatalf("runComponentReconcile() error = %v", err)
	}
	if got.Fcrepo != createpkg.FcrepoStateOn || got.Blazegraph != createpkg.FcrepoStateOn {
		t.Fatalf("healthy repository topology changed: fcrepo=%q blazegraph=%q", got.Fcrepo, got.Blazegraph)
	}
	if got.IIIF != createpkg.IIIFCantaloupe || got.IIIFTopology != createpkg.IIIFTopologyLocal {
		t.Fatalf("unexpected IIIF repair: implementation=%q topology=%q", got.IIIF, got.IIIFTopology)
	}
	if got.Codebase != createpkg.CodebaseNested {
		t.Fatalf("healthy codebase layout changed: %q", got.Codebase)
	}

	after, err := detectComponentViewsForContext(ctx, createpkg.DefaultDrupalRootfs)
	if err != nil {
		t.Fatalf("detectComponentViewsForContext(after) error = %v", err)
	}
	if drifted := driftedComponentViews(after); len(drifted) != 0 {
		t.Fatalf("drift remains after reconcile: %+v", drifted)
	}
	afterByName := componentViewsByName(after)
	for _, name := range []string{"fcrepo", "blazegraph", "iiif-topology", "codebase"} {
		beforeView := beforeByName[name]
		afterView := afterByName[name]
		if beforeView.State != afterView.State || beforeView.Disposition != afterView.Disposition {
			t.Fatalf("healthy component %q changed from %s/%s to %s/%s", name, beforeView.State, beforeView.Disposition, afterView.State, afterView.Disposition)
		}
	}
	if afterByName["iiif"].Disposition != corecomponent.DispositionCantaloupe {
		t.Fatalf("iiif drift repaired to %q, want %q", afterByName["iiif"].Disposition, corecomponent.DispositionCantaloupe)
	}

	compose = readFileForTest(t, composePath)
	for _, want := range []string{"\n  fcrepo:\n", "\n  blazegraph:\n", "context: ./drupal"} {
		if !strings.Contains(compose, want) {
			t.Fatalf("reconcile removed healthy topology marker %q:\n%s", want, compose)
		}
	}
	if strings.Contains(compose, "\n  triplet:\n") {
		t.Fatalf("reconcile did not remove drifted triplet service:\n%s", compose)
	}
}

func componentViewsByName(views []componentView) map[string]componentView {
	byName := make(map[string]componentView, len(views))
	for _, view := range views {
		byName[view.Name] = view
	}
	return byName
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
	cmd := newComponentReviewTestCommand()
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current disposition: `enabled`") {
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
	cmd := newComponentReviewTestCommand()
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "COMPONENT") || !strings.Contains(rendered, "blazegraph") {
		t.Fatalf("expected table report output, got:\n%s", rendered)
	}
}

func TestRunComponentReviewReportJSONFormatIncludesIngressDetails(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), `
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph:main
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: https://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_SITE_URL: https://repo.example.org
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
      DRUPAL_ENABLE_HTTPS: "true"
  fcrepo:
    image: islandora/fcrepo6
  traefik:
    command: >-
      --ping=true
      --entryPoints.http.address=:80
      --entryPoints.https.address=:443
      --certificatesResolvers.letsencrypt.acme.email=admin@example.org
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`)
	writeFileForTest(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), "http:\n  services:\n    drupal:\n      loadBalancer:\n        servers:\n          - url: http://drupal:80\n  routers:\n    drupal:\n      rule: Host(`repo.example.org`)\n      service: drupal\n      tls:\n        certResolver: letsencrypt\n")

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
	componentReviewFormat = corecomponent.ReportFormatJSON

	var out bytes.Buffer
	cmd := newComponentReviewTestCommand()
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput:\n%s", err, out.String())
	}

	var foundIngress bool
	for _, row := range rows {
		switch row["name"] {
		case "ingress":
			foundIngress = true
			followUps, _ := row["follow_ups"].(map[string]any)
			if followUps["mode"] != coretraefik.IngressModeHTTPSLetsEncrypt || followUps["domain"] != "repo.example.org" || followUps["acme-email"] != "admin@example.org" {
				t.Fatalf("expected ingress follow-ups, got %#v", row["follow_ups"])
			}
		}
	}
	if !foundIngress {
		t.Fatalf("expected ingress row in json output, got %#v", rows)
	}
}

func TestRunComponentReviewReportVerboseIncludesDriftDetails(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), `
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
      DRUPAL_ENABLE_HTTPS: "false"
  fcrepo:
    image: islandora/fcrepo6
volumes:
  fcrepo-data: {}
`)

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
	componentReviewVerbose = true
	componentReviewFormat = corecomponent.ReportFormatSection

	var out bytes.Buffer
	cmd := newComponentReviewTestCommand()
	cmd.SetOut(&out)

	if err := runComponentReview(cmd); err != nil {
		t.Fatalf("runComponentReview() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "Current disposition: `drifted`") || !strings.Contains(rendered, "drift:") {
		t.Fatalf("expected verbose drift details in report output, got:\n%s", rendered)
	}
}
