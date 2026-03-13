package cmd

import (
	"fmt"
	"strings"

	"github.com/libops/sitectl-isle/pkg/components"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

var (
	statusPath         string
	statusDrupalRootfs string
	statusVerbose      bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report ISLE component state for a checked out project",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(cmd)
	},
}

func init() {
	statusCmd.Flags().StringVar(&statusPath, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
	corecomponent.AddDrupalRootfsFlag(statusCmd, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	statusCmd.Flags().BoolVar(&statusVerbose, "verbose", false, "Show drift details for components that do not match cleanly")
}

func runStatus(cmd *cobra.Command) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}

	defs := []corecomponent.Definition{
		components.Fcrepo(components.TemplateSource{}),
		components.Blazegraph(components.TemplateSource{}),
	}

	statuses, err := corecomponent.DetectComponentStatuses(ctx, ctx.ProjectDir, corecomponent.DetectOptions{
		ComposeRoot:  ctx.ProjectDir,
		DrupalRootfs: statusDrupalRootfs,
	}, defs...)
	if err != nil {
		return err
	}

	for _, status := range statuses {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", status.Name, status.State)
		if statusVerbose && status.State == corecomponent.StateDrifted {
			writeDriftDetails(cmd, status)
		}
	}

	return nil
}

func resolveStatusContext() (*config.Context, error) {
	if strings.TrimSpace(statusPath) != "" {
		return &config.Context{
			DockerHostType: config.ContextLocal,
			ProjectDir:     statusPath,
		}, nil
	}
	if commandSDK == nil {
		return nil, fmt.Errorf("plugin sdk is not initialized")
	}
	ctx, err := commandSDK.GetContext()
	if err != nil {
		return nil, fmt.Errorf("resolve status context: %w", err)
	}
	if strings.TrimSpace(ctx.ProjectDir) == "" {
		return nil, fmt.Errorf("context %q does not define a project directory; pass --path or update the sitectl context", ctx.Name)
	}
	return ctx, nil
}

func writeDriftDetails(cmd *cobra.Command, status corecomponent.ComponentStatus) {
	printedHeader := false
	printFailures := func(label string, check corecomponent.StateCheck) {
		for _, result := range check.Results {
			if result.Match {
				continue
			}
			if !printedHeader {
				fmt.Fprintln(cmd.OutOrStdout(), "  drift:")
				printedHeader = true
			}
			fmt.Fprintf(cmd.OutOrStdout(), "    %s %s %s %s\n", label, result.Domain, result.File, strings.TrimSpace(result.Detail))
		}
	}
	printFailures("expected on:", status.On)
	printFailures("expected off:", status.Off)
}
