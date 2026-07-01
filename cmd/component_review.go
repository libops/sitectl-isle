package cmd

import (
	"context"
	"fmt"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/helpers"
	coredevmode "github.com/libops/sitectl/pkg/services/devmode"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
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
	componentReviewYolo              bool
)

type componentReviewDecision struct {
	Disposition   corecomponent.Disposition
	State         corecomponent.State
	TLSMode       string
	Domain        string
	ACMEEmail     string
	FileSystemURI string
	UpstreamURL   string
	TrustedIPs    string
	MaxUploadSize string
	UploadTimeout string
}

type promptReviewDecision = corecomponent.ReviewDecision

type componentReconcileOptions struct {
	ComponentName  string
	Path           string
	CodebaseRootfs string
	DrupalRootfs   string
	Report         bool
	Verbose        bool
	Format         string
	Yolo           bool
}

func componentReconcileOptionsFromGlobals(componentName string) componentReconcileOptions {
	return componentReconcileOptions{
		ComponentName:  componentName,
		Path:           statusPath,
		CodebaseRootfs: statusCodebaseRootfs,
		DrupalRootfs:   statusDrupalRootfs,
		Report:         componentReviewReport,
		Verbose:        componentReviewVerbose,
		Format:         componentReviewFormat,
		Yolo:           componentReviewYolo,
	}
}

func runComponentReview(cmd *cobra.Command) error {
	return runComponentReconcile(cmd, componentReconcileOptionsFromGlobals(componentReviewName))
}

func runComponentReconcile(cmd *cobra.Command, opts componentReconcileOptions) error {
	ctx, err := resolveStatusContextForPath(opts.Path)
	if err != nil {
		return err
	}
	if ctx.DockerHostType != config.ContextLocal {
		return fmt.Errorf("component review is local-only; context %q is %q", ctx.Name, ctx.DockerHostType)
	}
	rootfs, err := resolveCodebaseRootfsForContext(cmd, ctx, opts.CodebaseRootfs, opts.DrupalRootfs)
	if err != nil {
		return err
	}

	statuses, err := detectComponentViewsForContext(ctx, rootfs)
	if err != nil {
		return err
	}
	componentName := strings.TrimSpace(opts.ComponentName)
	if strings.TrimSpace(componentName) != "" {
		for _, status := range statuses {
			if status.Name == componentName {
				continue
			}
			if status.State == corecomponent.StateDrifted && blocksComponentSetOnDrift(componentName, status.Name) {
				return fmt.Errorf("component %q is drifted (%s); resolve it first or target it explicitly", status.Name, componentDriftSummary(status, 6))
			}
		}
	}
	statuses, err = filterComponentViews(statuses, componentName)
	if err != nil {
		return err
	}
	if opts.Report {
		return corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, opts.Verbose, opts.Format)
	}

	rawDecisions, err := corecomponent.RunReview(statuses, corecomponent.ReviewOptions{
		Input:             componentReviewInput,
		PromptState:       componentReviewPromptState,
		PromptDisposition: componentReviewPromptDisposition,
		PromptChoice:      componentReviewPromptChoice,
		SummaryLine:       componentReviewSummaryLine,
		Confirm: func(prompt string) (bool, error) {
			if opts.Yolo {
				return true, nil
			}
			return confirmComponentReview(prompt)
		},
	})
	if err != nil {
		return err
	}
	decisions := convertComponentReviewDecisions(rawDecisions)
	if strings.TrimSpace(componentName) != "" {
		decisions = mergeCurrentComponentReviewDecisions(ctx, rootfs, decisions)
	}

	if err := applyComponentReview(ctx, rootfs, opts.Path, decisions); err != nil {
		return err
	}

	for _, status := range statuses {
		decision := decisions[status.Name]
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s", status.Name, reviewDecisionLabel(decision))
		if strings.TrimSpace(decision.TLSMode) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.TLSMode)
		} else if strings.TrimSpace(decision.UpstreamURL) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.UpstreamURL)
		} else if strings.TrimSpace(decision.TrustedIPs) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", decision.TrustedIPs)
		} else if strings.TrimSpace(decision.MaxUploadSize) != "" || strings.TrimSpace(decision.UploadTimeout) != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s, %s)", uploadLimitValue(map[string]string{"max-upload-size": decision.MaxUploadSize}, "max-upload-size"), uploadLimitValue(map[string]string{"upload-timeout": decision.UploadTimeout}, "upload-timeout"))
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func mergeCurrentComponentReviewDecisions(ctx *config.Context, drupalRootfs string, decisions map[string]componentReviewDecision) map[string]componentReviewDecision {
	statuses, err := detectComponentViewsForContext(ctx, drupalRootfs)
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
			TLSMode:       strings.TrimSpace(helpers.FirstNonEmpty(status.FollowUpValues["mode"], status.FollowUpValues["tls-mode"])),
			Domain:        strings.TrimSpace(status.FollowUpValues["domain"]),
			ACMEEmail:     strings.TrimSpace(status.FollowUpValues["acme-email"]),
			FileSystemURI: strings.TrimSpace(status.FollowUpValues["isle-file-system-uri"]),
			UpstreamURL:   strings.TrimSpace(status.FollowUpValues["upstream-url"]),
			TrustedIPs:    strings.TrimSpace(status.FollowUpValues["trusted-ip"]),
			MaxUploadSize: strings.TrimSpace(status.FollowUpValues["max-upload-size"]),
			UploadTimeout: strings.TrimSpace(status.FollowUpValues["upload-timeout"]),
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
			TLSMode:       strings.TrimSpace(helpers.FirstNonEmpty(decision.Options["mode"], decision.Options["tls-mode"])),
			Domain:        strings.TrimSpace(decision.Options["domain"]),
			ACMEEmail:     strings.TrimSpace(decision.Options["acme-email"]),
			FileSystemURI: strings.TrimSpace(decision.Options["isle-file-system-uri"]),
			UpstreamURL:   strings.TrimSpace(decision.Options["upstream-url"]),
			TrustedIPs:    strings.TrimSpace(decision.Options["trusted-ip"]),
			MaxUploadSize: strings.TrimSpace(decision.Options["max-upload-size"]),
			UploadTimeout: strings.TrimSpace(decision.Options["upload-timeout"]),
		}
	}
	return decisions
}

func applyComponentReview(ctx *config.Context, drupalRootfs, pathOverride string, decisions map[string]componentReviewDecision) error {
	opts := createpkg.Options{
		Path:               ctx.ProjectDir,
		DrupalRootfs:       drupalRootfs,
		Fcrepo:             string(decisions["fcrepo"].State),
		Blazegraph:         string(decisions["blazegraph"].State),
		IIIF:               reviewIIIFCreateValue(decisions["iiif"]),
		IIIFTopology:       reviewIIIFTopologyCreateValue(decisions["iiif-topology"]),
		IIIFUpstreamURL:    strings.TrimSpace(decisions["iiif-topology"].UpstreamURL),
		ComposeOverride:    resolveEnvironmentOverridePath(ctx),
		DerivativeServices: reviewDerivativeServiceTopologies(decisions),
		Codebase:           reviewCodebaseCreateValue(decisions["codebase"]),
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
	if opts.Codebase == "" {
		opts.Codebase = createpkg.CodebaseNested
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
	if err := applyIngressReviewDecision(ctx, decisions[coretraefik.IngressName]); err != nil {
		return err
	}
	if err := applyDevModeReviewDecision(ctx, decisions[coredevmode.Name]); err != nil {
		return err
	}
	if err := updateContextRootfsForCodebase(ctx, pathOverride, "codebase", opts.Codebase); err != nil {
		return err
	}
	return ctx.EnsureTrackedComposeOverrideSymlink()
}

func applyIngressReviewDecision(ctx *config.Context, decision componentReviewDecision) error {
	if decision.State == "" {
		return nil
	}
	component, err := isleIngressComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	spec := component.SpecForWithOptions(decision.State, map[string]string{
		"mode":            strings.TrimSpace(decision.TLSMode),
		"domain":          strings.TrimSpace(decision.Domain),
		"acme-email":      strings.TrimSpace(decision.ACMEEmail),
		"trusted-ip":      strings.TrimSpace(decision.TrustedIPs),
		"max-upload-size": strings.TrimSpace(decision.MaxUploadSize),
		"upload-timeout":  strings.TrimSpace(decision.UploadTimeout),
	})
	switch decision.State {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEIngressFiles(ctx, map[string]string{
			"mode":   strings.TrimSpace(decision.TLSMode),
			"domain": strings.TrimSpace(decision.Domain),
		})
	default:
		return fmt.Errorf("unsupported ingress state %q", decision.State)
	}
}

func applyDevModeReviewDecision(ctx *config.Context, decision componentReviewDecision) error {
	if decision.State == "" {
		return nil
	}
	component, err := isleDevModeComponent()
	if err != nil {
		return err
	}
	manager := corecomponent.NewManager(ctx)
	spec := component.SpecForWithOptions(decision.State, nil)
	switch decision.State {
	case corecomponent.StateOn:
		if err := manager.EnableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEDevMode(ctx, true)
	case corecomponent.StateOff:
		if err := manager.DisableComponentWithOptions(context.Background(), spec, corecomponent.ApplyOptions{Yolo: true}); err != nil {
			return err
		}
		return applyISLEDevMode(ctx, false)
	default:
		return fmt.Errorf("unsupported dev mode state %q", decision.State)
	}
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

func reviewDerivativeServiceTopologies(decisions map[string]componentReviewDecision) map[string]string {
	out := map[string]string{}
	for _, name := range createpkg.DerivativeServiceNames() {
		decision, ok := decisions[name]
		if !ok {
			continue
		}
		if decision.Disposition == corecomponent.DispositionDistributed {
			out[name] = createpkg.DerivativeTopologyDistributed
		} else {
			out[name] = createpkg.DerivativeTopologyLocal
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func reviewCodebaseCreateValue(decision componentReviewDecision) string {
	if decision.Disposition == corecomponent.DispositionGitRoot {
		return createpkg.CodebaseGitRoot
	}
	return createpkg.CodebaseNested
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
