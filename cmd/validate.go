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

var (
	validateFormat string
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the ISLE project and context configuration",
	Long: `Validate the active context's configuration and project layout.

Checks include: context wiring, required Traefik configuration, Drupal rootfs path, and
component state consistency.

Exits non-zero if any check fails.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := resolveStatusContext()
		if err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}

		validators := append([]sitevalidate.Validator{}, sitevalidate.CoreValidators(cfg)...)
		if commandSDK != nil {
			validators = append(validators, commandSDK.ContextValidators()...)
		}

		results, err := sitevalidate.Run(ctx, validators...)
		if err != nil {
			return err
		}
		sitevalidate.SortResults(results)
		report := sitevalidate.NewReport(ctx, results)
		if err := sitevalidate.WriteReports(cmd.OutOrStdout(), []sitevalidate.Report{report}, validateFormat); err != nil {
			return err
		}
		if !report.Valid {
			return fmt.Errorf("validation failed")
		}
		return nil
	},
}

func init() {
	validateCmd.Flags().StringVar(&statusPath, "path", "", "Path to the project directory. Defaults to the active context project directory.")
	corecomponent.AddDrupalRootfsFlag(validateCmd, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	corecomponent.AddReportFlags(validateCmd, nil, &validateFormat)
}

// isleValidateRunner implements plugin.ValidateRunner for the isle plugin.
type isleValidateRunner struct {
	drupalRootfs string
}

func (r *isleValidateRunner) BindFlags(cmd *cobra.Command) {
	corecomponent.AddDrupalRootfsFlag(cmd, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
}

func (r *isleValidateRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	return runIsleValidation(ctx, r.drupalRootfs)
}

var _ plugin.ValidateRunner = (*isleValidateRunner)(nil)

func isleContextValidator(ctx *config.Context) ([]sitevalidate.Result, error) {
	return runIsleValidation(ctx, statusDrupalRootfs)
}

func runIsleValidation(ctx *config.Context, drupalRootfs string) ([]sitevalidate.Result, error) {
	if ctx == nil {
		return nil, nil
	}
	results := []sitevalidate.Result{}

	drupalRoot := ctx.ResolveProjectPath(drupalRootfs)
	if strings.TrimSpace(drupalRoot) == "" {
		results = append(results, sitevalidate.Result{
			Name:   "drupal-rootfs",
			Status: sitevalidate.StatusFailed,
			Detail: "Drupal rootfs path is empty",
		})
	} else if exists, err := ctx.FileExists(drupalRoot); err != nil {
		results = append(results, sitevalidate.Result{
			Name:   "drupal-rootfs",
			Status: sitevalidate.StatusFailed,
			Detail: err.Error(),
		})
	} else if !exists {
		results = append(results, sitevalidate.Result{
			Name:    "drupal-rootfs",
			Status:  sitevalidate.StatusFailed,
			Detail:  fmt.Sprintf("%s not found", drupalRoot),
			FixHint: "pass --drupal-rootfs or update the checkout layout",
		})
	} else {
		results = append(results, sitevalidate.Result{
			Name:   "drupal-rootfs",
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
			result.Detail = strings.TrimSpace(status.DriftDetail)
			if result.Detail == "" {
				result.Detail = strings.TrimSpace(status.Detail)
			}
			if result.Detail == "" {
				result.Detail = "component is drifted"
			}
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
