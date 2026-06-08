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
	componentName  string
	path           string
	report         bool
	verbose        bool
	format         string
	yolo           bool
	codebaseRootfs string
	drupalRootfs   string
}

func (r *isleConvergeRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&r.componentName, "component", "c", "", "Specific component to converge")
	cmd.Flags().StringVar(&r.path, "path", "", "Project path override")
	addCodebaseRootfsFlags(cmd, &r.codebaseRootfs, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	corecomponent.AddReviewFlags(cmd, &r.report, &r.verbose, &r.format)
	cmd.Flags().BoolVar(&r.yolo, "yolo", false, "Apply without confirmation")
}

func (r *isleConvergeRunner) Run(cmd *cobra.Command, ctx *config.Context) error {
	return runComponentReconcile(cmd, componentReconcileOptions{
		ComponentName:  r.componentName,
		Path:           r.path,
		CodebaseRootfs: r.codebaseRootfs,
		DrupalRootfs:   r.drupalRootfs,
		Report:         r.report,
		Verbose:        r.verbose,
		Format:         r.format,
		Yolo:           r.yolo,
	})
}

var _ plugin.ConvergeRunner = (*isleConvergeRunner)(nil)
