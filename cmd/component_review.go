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
	componentReviewInput        = config.GetInput
	componentReviewPromptState  = corecomponent.PromptState
	componentReviewPromptChoice = corecomponent.PromptChoice
	componentReviewReport       bool
	componentReviewVerbose      bool
	componentReviewFormat       string
)

type componentReviewDecision struct {
	State         corecomponent.State
	TLSMode       string
	FileSystemURI string
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
	corecomponent.AddReviewFlags(componentReviewCmd, &componentReviewReport, &componentReviewVerbose, &componentReviewFormat)
	componentCmd.AddCommand(componentReviewCmd)
}

func runComponentReview(cmd *cobra.Command) error {
	ctx, err := resolveStatusContext()
	if err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextLocal {
		return fmt.Errorf("component review is local-only; context %q is %q", ctx.Name, ctx.DockerHostType)
	}

	statuses, err := detectComponentViews(ctx.ProjectDir, statusDrupalRootfs)
	if err != nil {
		return err
	}
	if componentReviewReport {
		return corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, componentReviewVerbose, componentReviewFormat)
	}

	rawDecisions, err := corecomponent.RunReview(statuses, corecomponent.ReviewOptions{
		Input:        componentReviewInput,
		PromptState:  componentReviewPromptState,
		PromptChoice: componentReviewPromptChoice,
		SummaryLine:  componentReviewSummaryLine,
		Confirm:      confirmComponentReview,
	})
	if err != nil {
		return err
	}
	decisions := convertComponentReviewDecisions(rawDecisions)

	if err := applyComponentReview(ctx.ProjectDir, statusDrupalRootfs, decisions); err != nil {
		return err
	}

	for _, status := range statuses {
		decision := decisions[status.Name]
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", status.Name, decision.State)
		if strings.TrimSpace(decision.TLSMode) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.TLSMode)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func componentReviewSummaryLine(status componentView, decision promptReviewDecision) (string, error) {
	line := fmt.Sprintf("Set `%s` to `%s`.", status.Name, decision.State)
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
			State:         decision.State,
			TLSMode:       strings.TrimSpace(decision.Options["tls-mode"]),
			FileSystemURI: strings.TrimSpace(decision.Options["isle-file-system-uri"]),
		}
	}
	return decisions
}

func applyComponentReview(projectDir, drupalRootfs string, decisions map[string]componentReviewDecision) error {
	opts := createpkg.Options{
		Path:         projectDir,
		DrupalRootfs: drupalRootfs,
		Fcrepo:       string(decisions["fcrepo"].State),
		Blazegraph:   string(decisions["blazegraph"].State),
	}
	if opts.Fcrepo == "" {
		opts.Fcrepo = createpkg.FcrepoStateOn
	}
	if opts.Blazegraph == "" {
		opts.Blazegraph = createpkg.FcrepoStateOn
	}

	if opts.Fcrepo == createpkg.FcrepoStateOff {
		opts.ISLEFileSystemURI = strings.TrimSpace(decisions["fcrepo"].FileSystemURI)
		if opts.ISLEFileSystemURI == "" {
			scheme, err := resolveCurrentFileSystemURI(projectDir, drupalRootfs)
			if err != nil {
				return err
			}
			opts.ISLEFileSystemURI = scheme
		}
	}

	if err := componentApplyOptions(opts); err != nil {
		return err
	}
	if err := traefikconfig.ApplyProd(projectDir, reviewResolvedTLSMode("isle-tls", decisions["isle-tls"])); err != nil {
		return err
	}
	return traefikconfig.ApplyDev(projectDir, decisions["isle-tls-override"].State == corecomponent.StateOn, reviewResolvedTLSMode("isle-tls-override", decisions["isle-tls-override"]))
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
