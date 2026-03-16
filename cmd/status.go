package cmd

import (
	"fmt"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

var (
	statusPath         string
	statusDrupalRootfs string
	statusVerbose      bool
	statusFormat       string
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
	corecomponent.AddReportFlags(statusCmd, &statusVerbose, &statusFormat)
}

func runStatus(cmd *cobra.Command) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}

	statuses, err := detectComponentViews(ctx.ProjectDir, statusDrupalRootfs)
	if err != nil {
		return err
	}

	return corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, statusVerbose, statusFormat)
}

type componentView = corecomponent.ReviewView

func detectComponentViews(projectDir, drupalRootfs string) ([]componentView, error) {
	definitions := componentDefinitions()
	ctx := &config.Context{
		DockerHostType: config.ContextLocal,
		ProjectDir:     projectDir,
	}
	sdkStatuses, err := corecomponent.DetectComponentStatuses(ctx, projectDir, corecomponent.DetectOptions{
		ComposeRoot:  projectDir,
		DrupalRootfs: drupalRootfs,
	}, orderedComponentDefinitions()...)
	if err != nil {
		return nil, err
	}

	views := make([]componentView, 0, len(sdkStatuses)+2)
	for i := range sdkStatuses {
		followUps := map[string]string{}
		if sdkStatuses[i].Name == "fcrepo" && sdkStatuses[i].State == corecomponent.DetectedState(corecomponent.StateOff) {
			scheme, err := resolveCurrentFileSystemURI(projectDir, drupalRootfs)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(scheme) != "" {
				followUps["isle-file-system-uri"] = scheme
			}
		}
		views = append(views, componentView{
			Definition:     definitions[sdkStatuses[i].Name],
			Name:           sdkStatuses[i].Name,
			State:          sdkStatuses[i].State,
			SDKStatus:      &sdkStatuses[i],
			FollowUpValues: followUps,
		})
	}

	prodTLS, err := traefikconfig.DetectProd(projectDir)
	if err != nil {
		return nil, err
	}
	views = append(views, componentView{
		Definition:  definitions["isle-tls"],
		Name:        "isle-tls",
		State:       renderTLSDetectedState(prodTLS),
		Detail:      renderTLSDetail(prodTLS),
		DriftDetail: prodTLS.Detail,
		FollowUpValues: map[string]string{
			"tls-mode": strings.TrimSpace(prodTLS.Mode),
		},
		Extra: &prodTLS,
	})

	devTLS, err := traefikconfig.DetectDev(projectDir)
	if err != nil {
		return nil, err
	}
	views = append(views, componentView{
		Definition:  definitions["isle-tls-override"],
		Name:        "isle-tls-override",
		State:       renderTLSDetectedState(devTLS),
		Detail:      renderTLSDetail(devTLS),
		DriftDetail: devTLS.Detail,
		FollowUpValues: map[string]string{
			"tls-mode": strings.TrimSpace(devTLS.Mode),
		},
		Extra: &devTLS,
	})

	return views, nil
}

func renderTLSDetectedState(status traefikconfig.Status) corecomponent.DetectedState {
	if status.Drifted {
		return corecomponent.StateDrifted
	}
	if status.Enabled {
		return corecomponent.DetectedState(corecomponent.StateOn)
	}
	return corecomponent.DetectedState(corecomponent.StateOff)
}

func renderTLSDetail(status traefikconfig.Status) string {
	if status.Drifted {
		return strings.TrimSpace(status.Detail)
	}
	switch status.Mode {
	case "", traefikconfig.ModeInherited:
		return strings.TrimSpace(status.Detail)
	default:
		if strings.TrimSpace(status.Detail) == "" {
			return "mode=" + status.Mode
		}
		return fmt.Sprintf("mode=%s, %s", status.Mode, strings.TrimSpace(status.Detail))
	}
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
