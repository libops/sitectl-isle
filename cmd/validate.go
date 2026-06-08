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
	rootfs, err := resolveCodebaseRootfsFlag(cmd, r.codebaseRootfs, r.drupalRootfs)
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
	for _, status := range statuses {
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

	traefikPath := filepath.Join(ctx.ProjectDir, "conf", "traefik", "cantaloupe.yml")
	if exists, err := ctx.FileExists(traefikPath); err != nil {
		results = append(results, sitevalidate.Result{
			Name:   "traefik-cantaloupe-config",
			Status: sitevalidate.StatusFailed,
			Detail: err.Error(),
		})
	} else if !exists {
		results = append(results, sitevalidate.Result{
			Name:    "traefik-cantaloupe-config",
			Status:  sitevalidate.StatusFailed,
			Detail:  fmt.Sprintf("%s not found", traefikPath),
			FixHint: "ensure this is an ISLE checkout with the expected Traefik config",
		})
	} else {
		results = append(results, sitevalidate.Result{
			Name:   "traefik-cantaloupe-config",
			Status: sitevalidate.StatusOK,
			Detail: traefikPath,
		})
	}

	return results, nil
}
