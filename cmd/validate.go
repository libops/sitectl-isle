package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
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
