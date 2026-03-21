package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/externalcantaloupe"
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
	return runComponentDescribe(cmd, "", true)
}

func runComponentDescribe(cmd *cobra.Command, componentName string, includeIncluded bool) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}

	statuses, err := detectComponentViewsForContext(ctx, statusDrupalRootfs)
	if err != nil {
		return err
	}
	statuses, err = filterComponentViews(statuses, componentName)
	if err != nil {
		return err
	}

	if err := corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, statusVerbose, statusFormat); err != nil {
		return err
	}
	if includeIncluded && strings.TrimSpace(componentName) == "" && commandSDK != nil {
		outputs, err := commandSDK.InvokeIncludedPlugins([]string{"__component", "describe"})
		if err != nil {
			return err
		}
		for _, output := range outputs {
			if strings.TrimSpace(output) == "" {
				continue
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", output); err != nil {
				return err
			}
		}
	}
	return nil
}

type componentView = corecomponent.ReviewView

func detectComponentViewsForContext(siteCtx *config.Context, drupalRootfs string) ([]componentView, error) {
	definitions := componentDefinitions()
	sdkStatuses, err := corecomponent.DetectComponentStatuses(siteCtx, siteCtx.ProjectDir, corecomponent.DetectOptions{
		ComposeRoot:  siteCtx.ProjectDir,
		DrupalRootfs: drupalRootfs,
	}, orderedComponentDefinitions()...)
	if err != nil {
		return nil, err
	}

	views := make([]componentView, 0, len(sdkStatuses)+2)
	for i := range sdkStatuses {
		followUps := map[string]string{}
		disposition := dispositionFromDetectedState(sdkStatuses[i].State)
		if sdkStatuses[i].Name == "fcrepo" && sdkStatuses[i].State == corecomponent.DetectedState(corecomponent.StateOff) {
			disposition = corecomponent.DispositionSuperseded
			scheme, err := resolveCurrentFileSystemURI(siteCtx.ProjectDir, drupalRootfs)
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
			Disposition:    disposition,
			SDKStatus:      &sdkStatuses[i],
			FollowUpValues: followUps,
		})
	}

	externalStatus, err := externalcantaloupe.Detect(siteCtx.ProjectDir, resolveEnvironmentOverridePath(siteCtx))
	if err != nil {
		return nil, err
	}
	externalState := corecomponent.DetectedState(corecomponent.StateOff)
	if externalStatus.Drifted {
		externalState = corecomponent.StateDrifted
	} else if externalStatus.Enabled {
		externalState = corecomponent.DetectedState(corecomponent.StateOn)
	}
	views = append(views, componentView{
		Definition:  definitions["external-cantaloupe"],
		Name:        "external-cantaloupe",
		State:       externalState,
		Disposition: externalCantaloupeDisposition(externalStatus),
		Detail:      strings.TrimSpace(externalStatus.Detail),
		DriftDetail: strings.TrimSpace(externalStatus.Detail),
		FollowUpValues: map[string]string{
			"upstream-url": strings.TrimSpace(externalStatus.UpstreamURL),
		},
		Extra: &externalStatus,
	})

	prodTLS, err := traefikconfig.DetectProd(siteCtx.ProjectDir)
	if err != nil {
		return nil, err
	}
	views = append(views, componentView{
		Definition:  definitions["isle-tls"],
		Name:        "isle-tls",
		State:       renderTLSDetectedState(prodTLS),
		Disposition: dispositionFromDetectedState(renderTLSDetectedState(prodTLS)),
		Detail:      renderTLSDetail(prodTLS),
		DriftDetail: prodTLS.Detail,
		FollowUpValues: map[string]string{
			"tls-mode": strings.TrimSpace(prodTLS.Mode),
		},
		Extra: &prodTLS,
	})

	overridePath := resolveEnvironmentOverridePath(siteCtx)
	devTLS, err := traefikconfig.DetectOverride(siteCtx.ProjectDir, overridePath)
	if err != nil {
		return nil, err
	}
	views = append(views, componentView{
		Definition:  definitions["isle-tls-override"],
		Name:        "isle-tls-override",
		State:       renderTLSDetectedState(devTLS),
		Disposition: dispositionFromDetectedState(renderTLSDetectedState(devTLS)),
		Detail:      renderTLSDetail(devTLS),
		DriftDetail: devTLS.Detail,
		FollowUpValues: map[string]string{
			"tls-mode": strings.TrimSpace(devTLS.Mode),
		},
		Extra: &devTLS,
	})

	return views, nil
}

func dispositionFromDetectedState(state corecomponent.DetectedState) corecomponent.Disposition {
	switch state {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.DispositionEnabled
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.DispositionDisabled
	default:
		return ""
	}
}

func externalCantaloupeDisposition(status externalcantaloupe.Status) corecomponent.Disposition {
	switch {
	case status.Drifted:
		return ""
	case status.Enabled:
		return corecomponent.DispositionDistributed
	default:
		return corecomponent.DispositionDisabled
	}
}

func resolveEnvironmentOverridePath(siteCtx *config.Context) string {
	if siteCtx == nil {
		return ""
	}
	if path := strings.TrimSpace(siteCtx.TrackedComposeOverridePath()); path != "" {
		return path
	}
	return filepath.Join(siteCtx.ProjectDir, "docker-compose.local.yml")
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
		projectDir := filepath.Clean(statusPath)
		projectName := filepath.Base(projectDir)
		return &config.Context{
			DockerHostType: config.ContextLocal,
			Name:           projectName,
			Site:           projectName,
			Plugin:         "isle",
			Environment:    "local",
			DockerSocket:   config.GetDefaultLocalDockerSocket("/var/run/docker.sock"),
			ProjectName:    projectName,
			ProjectDir:     projectDir,
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

func filterComponentViews(statuses []componentView, componentName string) ([]componentView, error) {
	componentName = strings.TrimSpace(componentName)
	if componentName == "" {
		return statuses, nil
	}

	filtered := make([]componentView, 0, 1)
	for _, status := range statuses {
		if status.Name == componentName {
			filtered = append(filtered, status)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("unknown component %q", componentName)
	}
	return filtered, nil
}
