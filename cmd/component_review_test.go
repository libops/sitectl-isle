package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
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
	cmd.Flags().StringVar(&path, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
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
