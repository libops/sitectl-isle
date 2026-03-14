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

	statuses, err := detectComponentViews(ctx.ProjectDir, statusDrupalRootfs)
	if err != nil {
		return err
	}

	for i, status := range statuses {
		fmt.Fprintln(cmd.OutOrStdout(), renderComponentStatus(status))
		if statusVerbose && status.State == corecomponent.StateDrifted {
			if status.SDKStatus != nil {
				writeDriftDetails(cmd, *status.SDKStatus)
			} else if strings.TrimSpace(status.DriftDetail) != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "  drift:")
				fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", strings.TrimSpace(status.DriftDetail))
			}
		}
		if i < len(statuses)-1 {
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	return nil
}

type componentView struct {
	Definition  corecomponent.Definition
	Name        string
	State       corecomponent.DetectedState
	Detail      string
	DriftDetail string
	SDKStatus   *corecomponent.ComponentStatus
}

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
		views = append(views, componentView{
			Definition: definitions[sdkStatuses[i].Name],
			Name:       sdkStatuses[i].Name,
			State:      sdkStatuses[i].State,
			SDKStatus:  &sdkStatuses[i],
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

func renderComponentStatus(status componentView) string {
	lines := []string{
		fmt.Sprintf("Current state: `%s`", status.State),
	}
	if strings.TrimSpace(status.Detail) != "" {
		lines = append(lines, fmt.Sprintf("Detected mode: %s", status.Detail))
	}
	if guidance := renderCurrentGuidance(status); guidance != "" {
		lines = append(lines, "", guidance)
	}
	lines = append(lines,
		"",
		fmt.Sprintf("If enabled: %s", renderTransitionSummary(status.Definition.Behavior.Enable)),
		fmt.Sprintf("If disabled: %s", renderTransitionSummary(status.Definition.Behavior.Disable)),
	)
	return corecomponent.RenderSection(status.Name, strings.Join(lines, "\n"))
}

func renderCurrentGuidance(status componentView) string {
	switch status.State {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return strings.TrimSpace(status.Definition.Guidance.OnHelp)
	case corecomponent.DetectedState(corecomponent.StateOff):
		return strings.TrimSpace(status.Definition.Guidance.OffHelp)
	default:
		if strings.TrimSpace(status.Definition.Guidance.Question) != "" {
			return strings.TrimSpace(status.Definition.Guidance.Question)
		}
		return "This component does not match a clean on/off state right now."
	}
}

func renderTransitionSummary(behavior corecomponent.TransitionBehavior) string {
	summary := strings.TrimSpace(behavior.Summary)
	impact := renderMigrationImpact(behavior.DataMigration)
	switch {
	case summary == "" && impact == "":
		return "No additional behavior recorded."
	case summary == "":
		return impact + "."
	case impact == "":
		return summary
	default:
		return fmt.Sprintf("%s Impact: %s.", summary, impact)
	}
}

func renderMigrationImpact(migration corecomponent.DataMigrationRequirement) string {
	switch migration {
	case "", corecomponent.DataMigrationNone:
		return "low consequence"
	case corecomponent.DataMigrationBackfill:
		return "backfill likely required"
	case corecomponent.DataMigrationHard:
		return "high consequence, plan a data migration first"
	default:
		return string(migration)
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
