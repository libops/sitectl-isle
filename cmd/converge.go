package cmd

import (
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

// isleConvergeRunner implements plugin.ConvergeRunner for the isle plugin.
type isleConvergeRunner struct {
	componentName string
	report        bool
	verbose       bool
	format        string
	drupalRootfs  string
}

func (r *isleConvergeRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&r.componentName, "component", "c", "", "Specific component to converge")
	cmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	corecomponent.AddDrupalRootfsFlag(cmd, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	corecomponent.AddReviewFlags(cmd, &r.report, &r.verbose, &r.format)
}

func (r *isleConvergeRunner) Run(cmd *cobra.Command, ctx *config.Context) error {
	// Sync runner-bound rootfs into the package var that status helpers read.
	statusDrupalRootfs = r.drupalRootfs
	componentReviewReport = r.report
	componentReviewVerbose = r.verbose
	componentReviewFormat = r.format
	return runComponentReconcile(cmd, r.componentName)
}

var _ plugin.ConvergeRunner = (*isleConvergeRunner)(nil)
