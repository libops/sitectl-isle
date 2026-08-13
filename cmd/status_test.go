package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

func newStatusTestCommand() *cobra.Command {
	var path string
	var codebaseRootfs string
	var drupalRootfs string
	var verbose bool
	var format string

	cmd := &cobra.Command{Use: "status"}
	cmd.Flags().StringVar(&path, "path", "", "Path to the checked out ISLE project. Defaults to the active sitectl context project directory")
	addCodebaseRootfsFlags(cmd, &codebaseRootfs, &drupalRootfs, createpkg.DefaultDrupalRootfs)
	corecomponent.AddReportFlags(cmd, &verbose, &format)
	return cmd
}

func TestStatusCommandReportsOn(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current disposition: `enabled`") {
		t.Fatalf("expected blazegraph on, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo on, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "IIIF") || !strings.Contains(rendered, "Current disposition: `cantaloupe`") {
		t.Fatalf("expected iiif cantaloupe, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "IIIF-TOPOLOGY") || !strings.Contains(rendered, "Current disposition: `disabled`") {
		t.Fatalf("expected iiif topology local, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "INGRESS") || !strings.Contains(rendered, "Ingress mode: http") {
		t.Fatalf("expected ingress http, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "If enabled:") || !strings.Contains(rendered, "If disabled:") {
		t.Fatalf("expected transition guidance, got:\n%s", rendered)
	}
}

func TestStatusDetectsWrongRepositoryReactionValueAsDrift(t *testing.T) {
	tests := []struct {
		name          string
		componentName string
		file          string
		oldValue      string
		wrongValue    string
	}{
		{
			name:          "fcrepo reaction",
			componentName: "fcrepo",
			file:          "context.context.repository_content.yml",
			oldValue:      "index_node_in_fedora: index_node_in_fedora",
			wrongValue:    "index_node_in_fedora: wrong_action",
		},
		{
			name:          "blazegraph reaction",
			componentName: "blazegraph",
			file:          "context.context.repository_content.yml",
			oldValue:      "index_node_in_triplestore: index_node_in_triplestore",
			wrongValue:    "index_node_in_triplestore: wrong_action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			writeISLEOnFixture(t, projectDir)
			path := filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "config", "sync", tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", tt.file, err)
			}
			updated := strings.Replace(string(data), tt.oldValue, tt.wrongValue, 1)
			if updated == string(data) {
				t.Fatalf("fixture %s does not contain %q", tt.file, tt.oldValue)
			}
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				t.Fatalf("WriteFile(%s) error = %v", tt.file, err)
			}

			definitions := componentDefinitions()
			views, err := detectComponentViewsForDefinitions(&config.Context{
				Name:           "isle-local",
				Site:           "isle-local",
				Plugin:         "isle",
				DockerHostType: config.ContextLocal,
				ProjectDir:     projectDir,
			}, createpkg.DefaultDrupalRootfs, definitions[tt.componentName])
			if err != nil {
				t.Fatalf("detectComponentViewsForDefinitions() error = %v", err)
			}
			if len(views) != 1 {
				t.Fatalf("component views = %d, want 1", len(views))
			}
			if views[0].State != corecomponent.StateDrifted {
				t.Fatalf("%s state = %q, want %q", tt.componentName, views[0].State, corecomponent.StateDrifted)
			}
		})
	}
}

func TestApplyAndStatusAgreeForEveryRepositoryState(t *testing.T) {
	states := []struct {
		name                string
		fcrepo              string
		blazegraph          string
		wantFcrepoState     corecomponent.DetectedState
		wantBlazegraphState corecomponent.DetectedState
	}{
		{name: "both disabled", fcrepo: createpkg.FcrepoStateOff, blazegraph: createpkg.FcrepoStateOff, wantFcrepoState: corecomponent.DetectedState(corecomponent.StateOff), wantBlazegraphState: corecomponent.DetectedState(corecomponent.StateOff)},
		{name: "standalone blazegraph", fcrepo: createpkg.FcrepoStateOff, blazegraph: createpkg.FcrepoStateOn, wantFcrepoState: corecomponent.DetectedState(corecomponent.StateOff), wantBlazegraphState: corecomponent.DetectedState(corecomponent.StateOn)},
		{name: "standalone fcrepo", fcrepo: createpkg.FcrepoStateOn, blazegraph: createpkg.FcrepoStateOff, wantFcrepoState: corecomponent.DetectedState(corecomponent.StateOn), wantBlazegraphState: corecomponent.DetectedState(corecomponent.StateOff)},
		{name: "both enabled", fcrepo: createpkg.FcrepoStateOn, blazegraph: createpkg.FcrepoStateOn, wantFcrepoState: corecomponent.DetectedState(corecomponent.StateOn), wantBlazegraphState: corecomponent.DetectedState(corecomponent.StateOn)},
	}

	for _, tt := range states {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			writeISLEOnFixture(t, projectDir)
			opts := createpkg.Options{
				Path:              projectDir,
				DrupalRootfs:      createpkg.DefaultDrupalRootfs,
				Fcrepo:            tt.fcrepo,
				Blazegraph:        tt.blazegraph,
				ISLEFileSystemURI: createpkg.PrivateISLEFileSystemURI,
			}
			if err := createpkg.Apply(opts); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			configDir := filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "config", "sync")
			for _, name := range []string{
				"context.context.all_media.yml",
				"context.context.repository_content.yml",
				"context.context.taxonomy_terms.yml",
			} {
				path := filepath.Join(configDir, name)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("ReadFile(%s) error = %v", name, err)
				}
				doc, err := corecomponent.LoadYAMLDocument(data)
				if err != nil {
					t.Fatalf("LoadYAMLDocument(%s) error = %v", name, err)
				}
				for path, value := range map[string]string{
					".description": "Downstream-owned description",
					".reactions.index.actions.downstream_status_action": "downstream_status_action",
				} {
					if err := doc.SetString(path, value); err != nil {
						t.Fatalf("SetString(%s, %s) error = %v", name, path, err)
					}
				}
				updated, err := doc.Bytes()
				if err != nil {
					t.Fatalf("Bytes(%s) error = %v", name, err)
				}
				if err := os.WriteFile(path, updated, 0o644); err != nil {
					t.Fatalf("WriteFile(%s) error = %v", name, err)
				}
			}

			actionPath := filepath.Join(configDir, "system.action.delete_media_from_triplestore.yml")
			if tt.blazegraph == createpkg.FcrepoStateOn {
				data, err := os.ReadFile(actionPath)
				if err != nil {
					t.Fatalf("ReadFile(Blazegraph action) error = %v", err)
				}
				doc, err := corecomponent.LoadYAMLDocument(data)
				if err != nil {
					t.Fatalf("LoadYAMLDocument(Blazegraph action) error = %v", err)
				}
				if err := doc.SetString(".configuration.queue", "downstream-owned-queue"); err != nil {
					t.Fatalf("SetString(Blazegraph action queue) error = %v", err)
				}
				updated, err := doc.Bytes()
				if err != nil {
					t.Fatalf("Bytes(Blazegraph action) error = %v", err)
				}
				if err := os.WriteFile(actionPath, updated, 0o644); err != nil {
					t.Fatalf("WriteFile(Blazegraph action) error = %v", err)
				}
			}

			if err := createpkg.Apply(opts); err != nil {
				t.Fatalf("second Apply() error = %v", err)
			}
			definitions := componentDefinitions()
			views, err := detectComponentViewsForDefinitions(&config.Context{
				Name:           "isle-local",
				Site:           "isle-local",
				Plugin:         "isle",
				DockerHostType: config.ContextLocal,
				ProjectDir:     projectDir,
			}, createpkg.DefaultDrupalRootfs, definitions["fcrepo"], definitions["blazegraph"])
			if err != nil {
				t.Fatalf("detectComponentViewsForDefinitions() error = %v", err)
			}
			statesByName := map[string]corecomponent.DetectedState{}
			for _, view := range views {
				statesByName[view.Name] = view.State
			}
			if got := statesByName["fcrepo"]; got != tt.wantFcrepoState {
				t.Fatalf("fcrepo status = %q, want %q", got, tt.wantFcrepoState)
			}
			if got := statesByName["blazegraph"]; got != tt.wantBlazegraphState {
				t.Fatalf("blazegraph status = %q, want %q", got, tt.wantBlazegraphState)
			}
			for _, name := range []string{
				"context.context.all_media.yml",
				"context.context.repository_content.yml",
				"context.context.taxonomy_terms.yml",
			} {
				data, err := os.ReadFile(filepath.Join(configDir, name))
				if err != nil {
					t.Fatalf("ReadFile(preserved %s) error = %v", name, err)
				}
				if !strings.Contains(string(data), "Downstream-owned description") || !strings.Contains(string(data), "downstream_status_action") {
					t.Fatalf("Apply did not preserve unmanaged context fields in %s:\n%s", name, data)
				}
			}
			if tt.blazegraph == createpkg.FcrepoStateOn {
				data, err := os.ReadFile(actionPath)
				if err != nil {
					t.Fatalf("ReadFile(preserved Blazegraph action) error = %v", err)
				}
				if !strings.Contains(string(data), "downstream-owned-queue") {
					t.Fatalf("Apply did not preserve unmanaged Blazegraph action content:\n%s", data)
				}
			}
		})
	}
}

func TestStatusCommandReportsOff(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	if err := createpkg.Apply(createpkg.Options{
		Path:              projectDir,
		Fcrepo:            createpkg.FcrepoStateOff,
		Blazegraph:        createpkg.FcrepoStateOff,
		ISLEFileSystemURI: createpkg.PublicISLEFileSystemURI,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current disposition: `disabled`") {
		t.Fatalf("expected blazegraph off, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo off, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Drupal filesystem URI: public") {
		t.Fatalf("expected fcrepo filesystem follow-up, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "IIIF") || !strings.Contains(rendered, "Current disposition: `cantaloupe`") {
		t.Fatalf("expected iiif cantaloupe, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "IIIF-TOPOLOGY") || !strings.Contains(rendered, "Current disposition: `disabled`") {
		t.Fatalf("expected iiif topology local, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "INGRESS") || !strings.Contains(rendered, "Ingress mode: http") {
		t.Fatalf("expected ingress http, got:\n%s", rendered)
	}
}

func TestStatusCommandVerboseReportsDrift(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)
	composePath := filepath.Join(projectDir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  drupal:
    environment:
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: ""
  fcrepo:
    environment:
      DB_BOOTSTRAP_ENABLED: "true"
    image: islandora/fcrepo6
volumes:
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = true

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "Current disposition: `drifted`") {
		t.Fatalf("expected blazegraph drifted, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "drift:") {
		t.Fatalf("expected verbose drift details, got:\n%s", rendered)
	}
}

func TestStatusCommandUsesActiveContextProjectDir(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	if err := config.SaveContext(&config.Context{
		Name:           "isle-local",
		Site:           "isle-local",
		Plugin:         "isle",
		DockerHostType: config.ContextLocal,
		DockerSocket:   "/var/run/docker.sock",
		ProjectDir:     projectDir,
	}, true); err != nil {
		t.Fatalf("SaveContext() error = %v", err)
	}

	oldSDK := commandSDK
	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		commandSDK = oldSDK
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "test",
		Description: "test",
	})
	commandSDK.Config.Context = "isle-local"
	statusPath = ""
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "BLAZEGRAPH") || !strings.Contains(rendered, "Current disposition: `enabled`") {
		t.Fatalf("expected blazegraph on from active context project dir, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "FCREPO") {
		t.Fatalf("expected fcrepo on from active context project dir, got:\n%s", rendered)
	}
}

func TestComponentDescribeForwardsIncludedPluginSelectors(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldSDK := commandSDK
	oldStatusPath := statusPath
	oldStatusCodebaseRootfs := statusCodebaseRootfs
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	oldStatusFormat := statusFormat
	oldInvokeIncludedRPC := invokeIncludedRPC
	t.Cleanup(func() {
		commandSDK = oldSDK
		statusPath = oldStatusPath
		statusCodebaseRootfs = oldStatusCodebaseRootfs
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
		statusFormat = oldStatusFormat
		invokeIncludedRPC = oldInvokeIncludedRPC
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:     "isle",
		Version:  "test",
		Includes: []string{"drupal"},
	})
	statusPath = projectDir
	statusCodebaseRootfs = createpkg.DefaultDrupalRootfs
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = true
	statusFormat = "json"

	var gotInclude string
	var gotParams plugin.ComponentTargetParams
	invokeIncludedRPC = func(sdk *plugin.SDK, include string, req plugin.RPCRequest, opts plugin.CommandExecOptions) (plugin.RPCResponse, error) {
		gotInclude = include
		var err error
		gotParams, err = plugin.DecodeRPCParams[plugin.ComponentTargetParams](req.Params)
		if err != nil {
			return plugin.RPCResponse{}, err
		}
		return plugin.RPCResponse{OK: true, Output: "included output\n"}, nil
	}

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}
	if gotInclude != "drupal" {
		t.Fatalf("expected drupal include, got %q", gotInclude)
	}
	if gotParams.Path != projectDir {
		t.Fatalf("expected included path %q, got %q", projectDir, gotParams.Path)
	}
	if gotParams.CodebaseRootfs != createpkg.DefaultDrupalRootfs {
		t.Fatalf("expected included rootfs %q, got %q", createpkg.DefaultDrupalRootfs, gotParams.CodebaseRootfs)
	}
	if !gotParams.Verbose || gotParams.Format != "json" {
		t.Fatalf("expected verbose json included params, got %+v", gotParams)
	}
	if !strings.Contains(out.String(), "included output") {
		t.Fatalf("expected included output, got:\n%s", out.String())
	}
}

func TestResolveStatusContextUsesDotContextAsCWD(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	oldSDK := commandSDK
	t.Cleanup(func() {
		commandSDK = oldSDK
	})

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir(projectDir) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	commandSDK = plugin.NewSDK(plugin.Metadata{
		Name:        "isle",
		Version:     "test",
		Description: "test",
	})
	commandSDK.SetProjectDiscovery(func(projectDir string) (*config.ProjectClaim, error) {
		return &config.ProjectClaim{Plugin: "isle", ProjectDir: projectDir, Reason: "test claim"}, nil
	})
	commandSDK.Config.Context = "."

	ctx, err := resolveStatusContextForPath("")
	if err != nil {
		t.Fatalf("resolveStatusContextForPath() error = %v", err)
	}
	expectedProjectDir := projectDir
	if resolved, err := filepath.EvalSymlinks(expectedProjectDir); err == nil {
		expectedProjectDir = resolved
	}
	if ctx.ProjectDir != expectedProjectDir {
		t.Fatalf("expected project dir %q, got %q", expectedProjectDir, ctx.ProjectDir)
	}
	if ctx.Plugin != "isle" || ctx.DockerHostType != config.ContextLocal {
		t.Fatalf("unexpected context: %+v", ctx)
	}
}

func TestStatusCommandReportsIngressLetsEncryptMode(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(`
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
  milliner:
    image: islandora/milliner
  traefik:
    command: >-
      --ping=true
      --entryPoints.http.address=:80
      --entryPoints.https.address=:443
      --certificatesResolvers.letsencrypt.acme.email=admin@example.org
volumes:
  blazegraph-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), []byte("http:\n  services:\n    drupal:\n      loadBalancer:\n        servers:\n          - url: http://drupal:80\n  routers:\n    drupal:\n      rule: Host(`repo.example.org`)\n      service: drupal\n      tls:\n        certResolver: letsencrypt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(drupal router) error = %v", err)
	}

	oldStatusPath := statusPath
	oldStatusDrupalRootfs := statusDrupalRootfs
	oldStatusVerbose := statusVerbose
	t.Cleanup(func() {
		statusPath = oldStatusPath
		statusDrupalRootfs = oldStatusDrupalRootfs
		statusVerbose = oldStatusVerbose
	})
	statusPath = projectDir
	statusDrupalRootfs = createpkg.DefaultDrupalRootfs
	statusVerbose = false

	var out bytes.Buffer
	cmd := newStatusTestCommand()
	cmd.SetOut(&out)

	if err := runComponentDescribe(cmd, componentDescribeOptionsFromGlobals("", true)); err != nil {
		t.Fatalf("runComponentDescribe() error = %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "INGRESS") || !strings.Contains(rendered, "Ingress mode: https-letsencrypt") {
		t.Fatalf("expected ingress letsencrypt mode, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Domain: repo.example.org") || !strings.Contains(rendered, "ACME email: admin@example.org") {
		t.Fatalf("expected ingress follow-ups, got:\n%s", rendered)
	}
}

func TestCurrentIngressFollowUpsUsesLegacyDomainEnv(t *testing.T) {
	for _, tt := range []struct {
		name       string
		siteURL    string
		wantDomain string
	}{
		{
			name:       "expanded-site-url",
			siteURL:    "http://${DOMAIN}",
			wantDomain: "sandbox.islandora.ca",
		},
		{
			name:       "env-fallback",
			siteURL:    "",
			wantDomain: "sandbox.islandora.ca",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			siteURLEnvironment := ""
			if tt.siteURL != "" {
				siteURLEnvironment = "\n      DRUPAL_DEFAULT_SITE_URL: " + tt.siteURL
			}
			writeFileForTest(t, filepath.Join(projectDir, "compose.yaml"), `
services:
  drupal:
    environment:`+siteURLEnvironment+`
  traefik:
    command:
      - --entryPoints.http.address=:80
`)
			writeFileForTest(t, filepath.Join(projectDir, ".env"), "DOMAIN=sandbox.islandora.ca\n")

			got := currentIngressFollowUps(&config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
			if got["domain"] != tt.wantDomain {
				t.Fatalf("expected domain %q, got %#v", tt.wantDomain, got)
			}
		})
	}
}

func TestCurrentIngressFollowUpsPrefersTraefikRouteDomain(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "compose.yaml"), `
services:
  drupal:
    environment:
      DRUPAL_DEFAULT_SITE_URL: `+createpkg.LocalDrupalBaseURL+`
  traefik:
    command:
      - --entryPoints.http.address=:80
`)
	writeFileForTest(t, filepath.Join(projectDir, ".env"), "DOMAIN=sandbox.islandora.ca\n")
	if err := os.MkdirAll(filepath.Join(projectDir, "conf", "traefik"), 0o755); err != nil {
		t.Fatalf("MkdirAll(conf/traefik) error = %v", err)
	}
	writeFileForTest(t, filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), `
http:
  services:
    drupal:
      loadBalancer:
        servers:
          - url: http://drupal:80
  routers:
    drupal:
      rule: Host(`+"`"+`localhost`+"`"+`)
      service: drupal
`)

	got := currentIngressFollowUps(&config.Context{ProjectDir: projectDir, DockerHostType: config.ContextLocal})
	if got["domain"] != "localhost" {
		t.Fatalf("expected Traefik route domain localhost, got %#v", got)
	}
}

func TestIsleDefaultIngressDomainIsLocalOnly(t *testing.T) {
	t.Parallel()

	if got := isleDefaultIngressDomain(&config.Context{DockerHostType: config.ContextRemote}); got != "" {
		t.Fatalf("isleDefaultIngressDomain(remote) = %q, want empty", got)
	}
	if got := isleDefaultIngressDomain(&config.Context{DockerHostType: config.ContextLocal}); got != "localhost" {
		t.Fatalf("isleDefaultIngressDomain(local) = %q, want localhost", got)
	}
}

func TestRunIsleValidationAcceptsTripletIIIFConfig(t *testing.T) {
	projectDir := t.TempDir()
	writeISLEOnFixture(t, projectDir)

	if err := createpkg.ApplyIIIF(createpkg.Options{
		Path:         projectDir,
		DrupalRootfs: createpkg.DefaultDrupalRootfs,
		IIIF:         createpkg.IIIFTriplet,
	}); err != nil {
		t.Fatalf("ApplyIIIF() error = %v", err)
	}

	results, err := runIsleValidation(&config.Context{
		Name:           "isle-local",
		Site:           "isle-local",
		Plugin:         "isle",
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
	}, createpkg.DefaultDrupalRootfs)
	if err != nil {
		t.Fatalf("runIsleValidation() error = %v", err)
	}

	byName := map[string]sitevalidate.Result{}
	for _, result := range results {
		byName[result.Name] = result
	}
	for _, name := range []string{"component:iiif", "component:bot-mitigation", "traefik-triplet-config", "triplet-config"} {
		if byName[name].Status != sitevalidate.StatusOK {
			t.Fatalf("expected %s ok, got %+v", name, byName[name])
		}
	}
	if result, ok := byName["traefik-cantaloupe-config"]; ok && result.Status == sitevalidate.StatusFailed {
		t.Fatalf("did not expect Cantaloupe config failure for Triplet, got %+v", result)
	}
}

func writeISLEOnFixture(t *testing.T, projectDir string) {
	t.Helper()

	configDir := filepath.Join(projectDir, createpkg.DefaultDrupalRootfs, "config", "sync")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(`
services:
  alpaca:
    environment:
      ALPACA_FCREPO_INDEXER_ENABLED: "true"
      ALPACA_TRIPLESTORE_INDEXER_ENABLED: "true"
  blazegraph:
    image: islandora/blazegraph:main
  drupal:
    environment:
      DRUPAL_DEFAULT_CANTALOUPE_URL: http://localhost/cantaloupe/iiif/2
      DRUPAL_DEFAULT_FCREPO_URL: http://fcrepo.example/fcrepo/rest/
      DRUPAL_DEFAULT_SITE_URL: http://localhost
      DRUPAL_DEFAULT_TRIPLESTORE_NAMESPACE: islandora
      DRUPAL_ENABLE_HTTPS: "false"
      DRUSH_OPTIONS_URI: http://localhost
  cantaloupe:
    image: islandora/cantaloupe
  fcrepo:
    environment:
      DB_BOOTSTRAP_ENABLED: "true"
    image: islandora/fcrepo6
  milliner:
    image: islandora/milliner
  traefik:
    environment:
      CANTALOUPE_UPSTREAM_URL: http://cantaloupe:8182
    command:
      - --entryPoints.http.address=:80
      - --providers.file.directory=/etc/traefik/dynamic
      - --providers.file.watch=true
volumes:
  blazegraph-data: {}
  cantaloupe-data: {}
  fcrepo-data: {}
`), 0o644); err != nil {
		t.Fatalf("WriteFile(compose) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "conf", "traefik"), 0o755); err != nil {
		t.Fatalf("MkdirAll(conf/traefik) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "cantaloupe.yml"), []byte("http:\n  middlewares:\n    cantaloupe-strip-prefix:\n      stripPrefix:\n        prefixes:\n          - /cantaloupe\n    cantaloupe-custom-request-headers:\n      headers:\n        customRequestHeaders:\n          X-Forwarded-Path: /cantaloupe\n    cantaloupe:\n      chain:\n        middlewares:\n          - cantaloupe-strip-prefix\n          - cantaloupe-custom-request-headers\n\n  services:\n    cantaloupe:\n      loadBalancer:\n        servers:\n          - url: {{ env \"CANTALOUPE_UPSTREAM_URL\" }}\n  routers:\n    cantaloupe:\n      rule: Host(`localhost`) && PathPrefix(`/cantaloupe`)\n      middlewares:\n        - cantaloupe\n      service: cantaloupe\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(conf/traefik/cantaloupe.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "drupal.yml"), []byte("http:\n  services:\n    drupal:\n      loadBalancer:\n        servers:\n          - url: http://drupal:80\n  routers:\n    drupal:\n      rule: Host(`localhost`)\n      service: drupal\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(conf/traefik/drupal.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "conf", "traefik", "fcrepo.yml"), []byte("http:\n  middlewares:\n    fcrepo-strip-suffix:\n      replacePathRegex:\n        regex: \"^(.*/fcrepo/rest/[^.]*)/fcr:metadata$\"\n        replacement: \"$1\"\n  services:\n    fcrepo:\n      loadBalancer:\n        servers:\n          - url: http://fcrepo:8080\n  routers:\n    fcrepo:\n      rule: PathPrefix(`/fcrepo`)\n      middlewares:\n        - fcrepo-strip-suffix\n      service: fcrepo\n      priority: 100\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(conf/traefik/fcrepo.yml) error = %v", err)
	}

	files := []string{
		"context.context.all_media.yml",
		"context.context.external_files.yml",
		"context.context.repository_content.yml",
		"context.context.taxonomy_terms.yml",
		"system.action.delete_file_as_fedora_external_content.yml",
		"system.action.delete_node_from_fedora.yml",
		"system.action.delete_taxonomy_term_in_fedora.yml",
		"system.action.index_file_as_fedora_external_content.yml",
		"system.action.index_media_in_fedora.yml",
		"system.action.index_node_in_fedora.yml",
		"system.action.index_taxonomy_term_in_fedora.yml",
		"system.action.user_add_role_action.fedoraadmin.yml",
		"system.action.user_remove_role_action.fedoraadmin.yml",
		"user.role.fedoraadmin.yml",
		"views.view.non_fedora_files.yml",
		"system.action.delete_media_from_triplestore.yml",
		"system.action.delete_node_from_triplestore.yml",
		"system.action.delete_taxonomy_term_in_triplestore.yml",
		"system.action.index_media_in_triplestore.yml",
		"system.action.index_node_in_triplestore.yml",
		"system.action.index_taxonomy_term_in_the_triplestore.yml",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte("id: "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(configDir, "context.context.all_media.yml"), []byte("reactions:\n  index:\n    actions:\n      index_media_in_fedora: index_media_in_fedora\n      index_media_in_triplestore: index_media_in_triplestore\n  delete:\n    actions:\n      delete_media_from_triplestore: delete_media_from_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(all_media) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "context.context.repository_content.yml"), []byte("reactions:\n  index:\n    actions:\n      index_node_in_fedora: index_node_in_fedora\n      index_node_in_triplestore: index_node_in_triplestore\n  delete:\n    actions:\n      delete_node_from_fedora: delete_node_from_fedora\n      delete_node_from_triplestore: delete_node_from_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(repository_content) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "context.context.taxonomy_terms.yml"), []byte("reactions:\n  index:\n    actions:\n      index_taxonomy_term_in_fedora: index_taxonomy_term_in_fedora\n      index_taxonomy_term_in_the_triplestore: index_taxonomy_term_in_the_triplestore\n  delete:\n    actions:\n      delete_taxonomy_term_in_fedora: delete_taxonomy_term_in_fedora\n      delete_taxonomy_term_in_triplestore: delete_taxonomy_term_in_triplestore\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(taxonomy_terms) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "views.view.files.yml"), []byte("value: 'fedora://'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(views.view.files.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "jsonld.settings.yml"), []byte("namespace: 'http://fedora.info/definitions/v4/repository#'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(jsonld.settings.yml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "field.storage.media.field_media_file.yml"), []byte("settings:\n  uri_scheme: fedora\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(field.storage.media.field_media_file.yml) error = %v", err)
	}
}
