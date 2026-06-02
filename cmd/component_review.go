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
	componentReviewInput             = config.GetInput
	componentReviewPromptState       = corecomponent.PromptState
	componentReviewPromptDisposition corecomponent.PromptDispositionFunc
	componentReviewPromptChoice      = corecomponent.PromptChoice
	componentReviewName              string
	componentReviewReport            bool
	componentReviewVerbose           bool
	componentReviewFormat            string
)

type componentReviewDecision struct {
	Disposition   corecomponent.Disposition
	State         corecomponent.State
	TLSMode       string
	FileSystemURI string
	UpstreamURL   string
}

type promptReviewDecision = corecomponent.ReviewDecision

var componentReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review and adjust ISLE component state for the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentReview(cmd)
	},
}

func init() {
	componentReviewCmd.Flags().StringVar(&statusPath, "path", "", "Path to the checked out isle-site-template project. Defaults to the active sitectl context project directory")
	corecomponent.AddDrupalRootfsFlag(componentReviewCmd, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentReviewCmd.Flags().StringVarP(&componentReviewName, "component", "c", "", "Specific component to reconcile")
	corecomponent.AddReviewFlags(componentReviewCmd, &componentReviewReport, &componentReviewVerbose, &componentReviewFormat)
	componentCmd.AddCommand(componentReviewCmd)
}

func runComponentReview(cmd *cobra.Command) error {
	return runComponentReconcile(cmd, componentReviewName)
}

func runComponentReconcile(cmd *cobra.Command, componentName string) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextLocal {
		return fmt.Errorf("component review is local-only; context %q is %q", ctx.Name, ctx.DockerHostType)
	}

	statuses, err := detectComponentViewsForContext(ctx, statusDrupalRootfs)
	if err != nil {
		return err
	}
	if strings.TrimSpace(componentName) != "" {
		for _, status := range statuses {
			if status.Name == componentName {
				continue
			}
			if status.State == corecomponent.StateDrifted && blocksComponentSetOnDrift(status.Name) {
				return fmt.Errorf("component %q is drifted; resolve it first or target it explicitly", status.Name)
			}
		}
	}
	statuses, err = filterComponentViews(statuses, componentName)
	if err != nil {
		return err
	}
	if componentReviewReport {
		return corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, componentReviewVerbose, componentReviewFormat)
	}

	rawDecisions, err := corecomponent.RunReview(statuses, corecomponent.ReviewOptions{
		Input:             componentReviewInput,
		PromptState:       componentReviewPromptState,
		PromptDisposition: componentReviewPromptDisposition,
		PromptChoice:      componentReviewPromptChoice,
		SummaryLine:       componentReviewSummaryLine,
		Confirm:           confirmComponentReview,
	})
	if err != nil {
		return err
	}
	decisions := convertComponentReviewDecisions(rawDecisions)
	if strings.TrimSpace(componentName) != "" {
		decisions = mergeCurrentComponentReviewDecisions(ctx, decisions)
	}

	if err := applyComponentReview(ctx, statusDrupalRootfs, decisions); err != nil {
		return err
	}

	for _, status := range statuses {
		decision := decisions[status.Name]
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", status.Name, reviewDecisionLabel(decision))
		if strings.TrimSpace(decision.TLSMode) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.TLSMode)
		} else if strings.TrimSpace(decision.UpstreamURL) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.UpstreamURL)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func mergeCurrentComponentReviewDecisions(ctx *config.Context, decisions map[string]componentReviewDecision) map[string]componentReviewDecision {
	statuses, err := detectComponentViewsForContext(ctx, statusDrupalRootfs)
	if err != nil {
		return decisions
	}

	merged := make(map[string]componentReviewDecision, len(statuses))
	for name, decision := range decisions {
		merged[name] = decision
	}

	for _, status := range statuses {
		if _, ok := merged[status.Name]; ok {
			continue
		}
		decision := componentReviewDecision{
			Disposition:   status.Disposition,
			State:         corecomponent.DispositionToState(status.Disposition),
			TLSMode:       strings.TrimSpace(status.FollowUpValues["tls-mode"]),
			FileSystemURI: strings.TrimSpace(status.FollowUpValues["isle-file-system-uri"]),
			UpstreamURL:   strings.TrimSpace(status.FollowUpValues["upstream-url"]),
		}
		if decision.State == "" {
			switch status.State {
			case corecomponent.DetectedState(corecomponent.StateOn):
				decision.State = corecomponent.StateOn
			case corecomponent.DetectedState(corecomponent.StateOff):
				decision.State = corecomponent.StateOff
			}
		}
		if decision.Disposition == "" && decision.State != "" {
			decision.Disposition = corecomponent.StateToDisposition(decision.State)
		}
		merged[status.Name] = decision
	}

	return merged
}

func componentReviewSummaryLine(status componentView, decision promptReviewDecision) (string, error) {
	line := fmt.Sprintf("Set `%s` to `%s`.", status.Name, reviewPromptDecisionLabel(decision))
	if rendered := corecomponent.RenderDecisionFollowUps(status.Definition, decision); rendered != "" {
		line = fmt.Sprintf("%s %s", line, rendered)
	}
	return line, nil
}

func confirmComponentReview(prompt string) (bool, error) {
	response, err := componentReviewInput(prompt)
	if err != nil {
		return false, err
	}
	value := strings.TrimSpace(strings.ToLower(response))
	return value == "y" || value == "yes", nil
}

func convertComponentReviewDecisions(raw map[string]promptReviewDecision) map[string]componentReviewDecision {
	decisions := make(map[string]componentReviewDecision, len(raw))
	for name, decision := range raw {
		decisions[name] = componentReviewDecision{
			Disposition:   decision.Disposition,
			State:         decision.State,
			TLSMode:       strings.TrimSpace(decision.Options["tls-mode"]),
			FileSystemURI: strings.TrimSpace(decision.Options["isle-file-system-uri"]),
			UpstreamURL:   strings.TrimSpace(decision.Options["upstream-url"]),
		}
	}
	return decisions
}

func applyComponentReview(ctx *config.Context, drupalRootfs string, decisions map[string]componentReviewDecision) error {
	opts := createpkg.Options{
		Path:            ctx.ProjectDir,
		DrupalRootfs:    drupalRootfs,
		Fcrepo:          string(decisions["fcrepo"].State),
		Blazegraph:      string(decisions["blazegraph"].State),
		IIIF:            reviewIIIFCreateValue(decisions["iiif"]),
		IIIFTopology:    reviewIIIFTopologyCreateValue(decisions["iiif-topology"]),
		IIIFUpstreamURL: strings.TrimSpace(decisions["iiif-topology"].UpstreamURL),
		ComposeOverride: resolveEnvironmentOverridePath(ctx),
	}
	if opts.Fcrepo == "" {
		opts.Fcrepo = createpkg.FcrepoStateOn
	}
	if opts.Blazegraph == "" {
		opts.Blazegraph = createpkg.FcrepoStateOn
	}
	if opts.IIIF == "" {
		opts.IIIF = createpkg.IIIFCantaloupe
	}
	if opts.IIIFTopology == "" {
		opts.IIIFTopology = createpkg.IIIFTopologyLocal
	}

	if opts.Fcrepo == createpkg.FcrepoStateOff {
		opts.ISLEFileSystemURI = strings.TrimSpace(decisions["fcrepo"].FileSystemURI)
		if opts.ISLEFileSystemURI == "" {
			scheme, err := resolveCurrentFileSystemURI(ctx.ProjectDir, drupalRootfs)
			if err != nil {
				return err
			}
			opts.ISLEFileSystemURI = scheme
		}
	}

	if err := componentApplyOptions(opts); err != nil {
		return err
	}
	if err := traefikconfig.ApplyProd(ctx.ProjectDir, reviewResolvedTLSMode("isle-tls", decisions["isle-tls"])); err != nil {
		return err
	}
	if err := traefikconfig.ApplyOverride(ctx.ProjectDir, resolveEnvironmentOverridePath(ctx), decisions["isle-tls-override"].State == corecomponent.StateOn, reviewResolvedTLSMode("isle-tls-override", decisions["isle-tls-override"])); err != nil {
		return err
	}
	return ctx.EnsureTrackedComposeOverrideSymlink()
}

func reviewIIIFCreateValue(decision componentReviewDecision) string {
	if decision.Disposition == corecomponent.DispositionTriplet {
		return createpkg.IIIFTriplet
	}
	return createpkg.IIIFCantaloupe
}

func reviewIIIFTopologyCreateValue(decision componentReviewDecision) string {
	if decision.Disposition == corecomponent.DispositionDistributed {
		return createpkg.IIIFTopologyExternal
	}
	return createpkg.IIIFTopologyLocal
}

func reviewDecisionLabel(decision componentReviewDecision) string {
	if decision.Disposition != "" {
		return string(decision.Disposition)
	}
	return string(corecomponent.StateToDisposition(decision.State))
}

func reviewPromptDecisionLabel(decision promptReviewDecision) string {
	if decision.Disposition != "" {
		return string(decision.Disposition)
	}
	return string(corecomponent.StateToDisposition(decision.State))
}

func reviewResolvedTLSMode(name string, decision componentReviewDecision) string {
	if decision.State != corecomponent.StateOn {
		if name == "isle-tls" {
			return traefikconfig.ModeHTTP
		}
		return traefikconfig.ModeInherited
	}
	if strings.TrimSpace(decision.TLSMode) != "" {
		return decision.TLSMode
	}
	mode, err := defaultTLSPromptMode(name)
	if err != nil {
		return ""
	}
	return mode
}
