package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// isleValidateRunner implements plugin.ValidateRunner for the isle plugin.
type isleValidateRunner struct {
	codebaseRootfs string
	drupalRootfs   string
}

func (r *isleValidateRunner) BindFlags(cmd *cobra.Command) {
	addCodebaseRootfsFlags(cmd, &r.codebaseRootfs, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
}

func (r *isleValidateRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	rootfs, err := resolveCodebaseRootfsForContext(cmd, ctx, r.codebaseRootfs, r.drupalRootfs)
	if err != nil {
		return nil, err
	}
	return runIsleValidation(ctx, rootfs)
}

var _ plugin.ValidateRunner = (*isleValidateRunner)(nil)

func runIsleValidation(ctx *config.Context, drupalRootfs string) ([]sitevalidate.Result, error) {
	if ctx == nil {
		return nil, nil
	}
	results := []sitevalidate.Result{}

	drupalRoot := ctx.ResolveProjectPath(drupalRootfs)
	if strings.TrimSpace(drupalRoot) == "" {
		results = append(results, sitevalidate.Result{
			Name:   "codebase-rootfs",
			Status: sitevalidate.StatusFailed,
			Detail: "Drupal rootfs path is empty",
		})
	} else if exists, err := ctx.FileExists(drupalRoot); err != nil {
		results = append(results, sitevalidate.Result{
			Name:   "codebase-rootfs",
			Status: sitevalidate.StatusFailed,
			Detail: err.Error(),
		})
	} else if !exists {
		results = append(results, sitevalidate.Result{
			Name:    "codebase-rootfs",
			Status:  sitevalidate.StatusFailed,
			Detail:  fmt.Sprintf("%s not found", drupalRoot),
			FixHint: "pass --codebase-rootfs or update the checkout layout",
		})
	} else {
		results = append(results, sitevalidate.Result{
			Name:   "codebase-rootfs",
			Status: sitevalidate.StatusOK,
			Detail: drupalRoot,
		})
	}
	results = append(results, validateDatabaseSecretBoundary(ctx))

	if ctx.DockerHostType != config.ContextLocal {
		results = append(results, sitevalidate.Result{
			Name:   "isle-component-layout",
			Status: sitevalidate.StatusWarning,
			Detail: "ISLE stack-specific file validation is currently local-only",
		})
		return results, nil
	}

	statuses, err := detectComponentViewsForContext(ctx, drupalRootfs)
	if err != nil {
		return nil, err
	}
	statusByName := map[string]componentView{}
	for _, status := range statuses {
		statusByName[status.Name] = status
		result := sitevalidate.Result{
			Name:   "component:" + status.Name,
			Status: sitevalidate.StatusOK,
			Detail: string(status.State),
		}
		switch status.State {
		case corecomponent.StateDrifted:
			result.Status = sitevalidate.StatusFailed
			result.Detail = componentDriftSummary(status, 6)
			result.FixHint = "run `sitectl converge --report` to preview or `sitectl converge` to apply"
		}
		results = append(results, result)
	}
	for _, name := range createpkg.FeatureBundleNames() {
		status, ok := statusByName[name]
		if !ok || status.State != corecomponent.DetectedState(corecomponent.StateOn) {
			continue
		}
		result := sitevalidate.Result{
			Name:   "feature-requirements:" + name,
			Status: sitevalidate.StatusOK,
			Detail: "Compose compatibility requirements satisfied",
		}
		if err := createpkg.CheckFeatureBundleProject(createpkg.Options{
			Path:         ctx.ProjectDir,
			DrupalRootfs: drupalRootfs,
			EnvFiles:     append([]string{}, ctx.EnvFile...),
			FeatureBundleOptions: map[string]map[string]string{
				name: createpkg.FeatureBundleCurrentOptions(ctx.ProjectDir, drupalRootfs, ctx.EnvFile, name),
			},
		}, name); err != nil {
			result.Status = sitevalidate.StatusFailed
			result.Detail = err.Error()
			result.FixHint = "update the referenced Compose image or required secret definitions, then rerun validation"
		}
		results = append(results, result)
	}

	if statusByName["iiif"].Disposition == corecomponent.DispositionTriplet {
		results = append(results,
			validateProjectFile(ctx, "traefik-triplet-config", filepath.Join("conf", "traefik", "triplet.yml")),
			validateProjectFile(ctx, "triplet-config", filepath.Join("conf", "triplet", "config.yaml")),
		)
	} else {
		results = append(results,
			validateProjectFile(ctx, "traefik-cantaloupe-config", filepath.Join("conf", "traefik", "cantaloupe.yml")),
		)
	}

	return results, nil
}

func validateDatabaseSecretBoundary(ctx *config.Context) sitevalidate.Result {
	result := sitevalidate.Result{
		Name:    "database-secret-boundary",
		Status:  sitevalidate.StatusOK,
		Detail:  "root credentials are limited to MariaDB and one-shot database initializers",
		FixHint: "run `sitectl converge` to restore one-shot database initialization and scoped application secrets",
	}
	data, err := ctx.ReadSmallFile(filepath.Join(ctx.ProjectDir, "compose.yaml"))
	if err != nil {
		result.Status = sitevalidate.StatusFailed
		result.Detail = err.Error()
		return result
	}
	var document map[string]any
	if err := yaml.Unmarshal([]byte(data), &document); err != nil {
		result.Status = sitevalidate.StatusFailed
		result.Detail = fmt.Sprintf("parse compose.yaml: %v", err)
		return result
	}
	services := validationYAMLMap(document["services"])
	problems := []string{}
	allowedRoot := map[string]bool{"mariadb": true, "database-init": true, "fcrepo-database-init": true}
	for name, raw := range services {
		if serviceHasComposeSecret(validationYAMLMap(raw), "DB_ROOT_PASSWORD") && !allowedRoot[name] {
			problems = append(problems, name+" receives DB_ROOT_PASSWORD")
		}
	}
	for _, service := range []string{"mariadb", "database-init", "drupal"} {
		if validationYAMLMap(services[service]) == nil {
			problems = append(problems, "missing "+service+" service")
		}
	}
	if databaseInit := validationYAMLMap(services["database-init"]); databaseInit != nil {
		if !serviceHasComposeSecret(databaseInit, "DB_ROOT_PASSWORD") || !serviceHasComposeSecret(databaseInit, "DRUPAL_DEFAULT_DB_PASSWORD") {
			problems = append(problems, "database-init lacks root or scoped Drupal database secret")
		}
	}
	if drupal := validationYAMLMap(services["drupal"]); drupal != nil {
		if !serviceHasComposeSecret(drupal, "DRUPAL_DEFAULT_DB_PASSWORD") {
			problems = append(problems, "drupal lacks its scoped database secret")
		}
		if !serviceDependsOn(drupal, "database-init") {
			problems = append(problems, "drupal does not wait for database-init")
		}
	}
	fcrepo := validationYAMLMap(services["fcrepo"])
	fcrepoInit := validationYAMLMap(services["fcrepo-database-init"])
	if fcrepo != nil {
		if fcrepoInit == nil {
			problems = append(problems, "fcrepo is enabled without fcrepo-database-init")
		} else if !serviceHasComposeSecret(fcrepoInit, "DB_ROOT_PASSWORD") || !serviceHasComposeSecret(fcrepoInit, "FCREPO_DB_PASSWORD") {
			problems = append(problems, "fcrepo-database-init lacks root or scoped Fcrepo database secret")
		}
		if !serviceHasComposeSecret(fcrepo, "FCREPO_DB_PASSWORD") {
			problems = append(problems, "fcrepo lacks its scoped database secret")
		}
		if !serviceDependsOn(fcrepo, "fcrepo-database-init") {
			problems = append(problems, "fcrepo does not wait for fcrepo-database-init")
		}
	} else if fcrepoInit != nil {
		problems = append(problems, "fcrepo-database-init remains while fcrepo is disabled")
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		result.Status = sitevalidate.StatusFailed
		result.Detail = strings.Join(problems, "; ")
	}
	return result
}

func validationYAMLMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

func serviceHasComposeSecret(service map[string]any, source string) bool {
	secrets, _ := service["secrets"].([]any)
	for _, raw := range secrets {
		if name, ok := raw.(string); ok && name == source {
			return true
		}
		if name, ok := validationYAMLMap(raw)["source"].(string); ok && name == source {
			return true
		}
	}
	return false
}

func serviceDependsOn(service map[string]any, dependency string) bool {
	depends := service["depends_on"]
	if mapping := validationYAMLMap(depends); mapping != nil {
		_, ok := mapping[dependency]
		return ok
	}
	if sequence, ok := depends.([]any); ok {
		for _, raw := range sequence {
			if name, ok := raw.(string); ok && name == dependency {
				return true
			}
		}
	}
	return false
}

func validateProjectFile(ctx *config.Context, name, relPath string) sitevalidate.Result {
	path := filepath.Join(ctx.ProjectDir, relPath)
	if exists, err := ctx.FileExists(path); err != nil {
		return sitevalidate.Result{
			Name:   name,
			Status: sitevalidate.StatusFailed,
			Detail: err.Error(),
		}
	} else if !exists {
		return sitevalidate.Result{
			Name:    name,
			Status:  sitevalidate.StatusFailed,
			Detail:  fmt.Sprintf("%s not found", path),
			FixHint: "ensure this is an ISLE checkout with the expected config",
		}
	}
	return sitevalidate.Result{
		Name:   name,
		Status: sitevalidate.StatusOK,
		Detail: path,
	}
}
