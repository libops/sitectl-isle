package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl-isle/pkg/traefikconfig"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

var (
	statusPath           string
	statusCodebaseRootfs string
	statusDrupalRootfs   string
	statusVerbose        bool
	statusFormat         string
	invokeIncludedRPC    = invokeSDKIncludedRPC
)

type componentDescribeOptions struct {
	ComponentName   string
	IncludeIncluded bool
	Path            string
	CodebaseRootfs  string
	DrupalRootfs    string
	Verbose         bool
	Format          string
}

func componentDescribeOptionsFromGlobals(componentName string, includeIncluded bool) componentDescribeOptions {
	return componentDescribeOptions{
		ComponentName:   componentName,
		IncludeIncluded: includeIncluded,
		Path:            statusPath,
		CodebaseRootfs:  statusCodebaseRootfs,
		DrupalRootfs:    statusDrupalRootfs,
		Verbose:         statusVerbose,
		Format:          statusFormat,
	}
}

func runComponentDescribe(cmd *cobra.Command, opts componentDescribeOptions) error {
	ctx, err := resolveStatusContextForPath(opts.Path)
	if err != nil {
		return err
	}
	rootfs, err := resolveCodebaseRootfsFlag(cmd, opts.CodebaseRootfs, opts.DrupalRootfs)
	if err != nil {
		return err
	}

	statuses, err := detectComponentViewsForContext(ctx, rootfs)
	if err != nil {
		return err
	}
	componentName := strings.TrimSpace(opts.ComponentName)
	statuses, err = filterComponentViews(statuses, componentName)
	if err != nil {
		return err
	}

	if err := corecomponent.WriteComponentStatusReportWithFormat(cmd.OutOrStdout(), statuses, opts.Verbose, opts.Format); err != nil {
		return err
	}
	// Included plugins are appended only for the full default view. A targeted
	// ISLE component describe should not fail because an included plugin does
	// not know that component name.
	if opts.IncludeIncluded && componentName == "" && commandSDK != nil {
		for _, include := range commandSDK.Metadata.Includes {
			req, err := plugin.NewComponentDescribeRequest(plugin.ComponentTargetParams{
				Path:           strings.TrimSpace(opts.Path),
				CodebaseRootfs: strings.TrimSpace(rootfs),
				Verbose:        opts.Verbose,
				Format:         strings.TrimSpace(opts.Format),
			})
			if err != nil {
				return err
			}
			resp, err := invokeIncludedRPC(commandSDK, include, req, plugin.CommandExecOptions{
				Context: cmd.Context(),
			})
			if err != nil {
				return err
			}
			output := resp.Output
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

func invokeSDKIncludedRPC(sdk *plugin.SDK, include string, req plugin.RPCRequest, opts plugin.CommandExecOptions) (plugin.RPCResponse, error) {
	return sdk.InvokeIncludedPluginRPC(include, req, opts)
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
		switch sdkStatuses[i].Name {
		case "fcrepo":
			if sdkStatuses[i].State != corecomponent.DetectedState(corecomponent.StateOff) {
				break
			}
			disposition = corecomponent.DispositionSuperseded
			scheme, err := resolveCurrentFileSystemURI(siteCtx.ProjectDir, drupalRootfs)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(scheme) != "" {
				followUps["isle-file-system-uri"] = scheme
			}
		case "iiif":
			disposition = iiifDisposition(sdkStatuses[i].State)
		case "iiif-topology":
			disposition = iiifTopologyDisposition(sdkStatuses[i].State)
			if upstream := currentIIIFUpstreamURL(siteCtx.ProjectDir); strings.TrimSpace(upstream) != "" {
				followUps["upstream-url"] = strings.TrimSpace(upstream)
			}
		default:
			if createpkg.IsDerivativeService(sdkStatuses[i].Name) {
				disposition = derivativeServiceDisposition(sdkStatuses[i].State)
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

func iiifDisposition(state corecomponent.DetectedState) corecomponent.Disposition {
	switch state {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.DispositionTriplet
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.DispositionCantaloupe
	default:
		return ""
	}
}

func iiifTopologyDisposition(state corecomponent.DetectedState) corecomponent.Disposition {
	switch state {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.DispositionDistributed
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.DispositionDisabled
	default:
		return ""
	}
}

func derivativeServiceDisposition(state corecomponent.DetectedState) corecomponent.Disposition {
	switch state {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.DispositionDistributed
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.DispositionEnabled
	default:
		return ""
	}
}

func currentIIIFUpstreamURL(projectDir string) string {
	compose, err := corecomponent.LoadComposeFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		return ""
	}
	return composeServiceEnvValue(compose, "traefik", "IIIF_UPSTREAM_URL")
}

func composeServiceEnvValue(compose *corecomponent.ComposeFile, service, key string) string {
	if compose == nil {
		return ""
	}
	block, ok := compose.ServiceBlock(service)
	if !ok {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "environment:" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			line := lines[j]
			if strings.TrimSpace(line) == "" {
				continue
			}
			if leadingSpaces(line) <= 4 {
				break
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, key+":") {
				continue
			}
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				return ""
			}
			return strings.Trim(strings.TrimSpace(parts[1]), `"`)
		}
	}
	return ""
}

func leadingSpaces(value string) int {
	return len(value) - len(strings.TrimLeft(value, " "))
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

func resolveStatusContextForPath(path string) (*config.Context, error) {
	if strings.TrimSpace(path) != "" {
		return localStatusContext(path)
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

func localStatusContext(projectDir string) (*config.Context, error) {
	projectDir = filepath.Clean(projectDir)
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project directory %q: %w", projectDir, err)
	}
	if _, err := os.Stat(absProjectDir); err != nil {
		return nil, fmt.Errorf("stat project directory %q: %w", absProjectDir, err)
	}
	projectName := filepath.Base(absProjectDir)
	return &config.Context{
		DockerHostType: config.ContextLocal,
		Name:           projectName,
		Site:           projectName,
		Plugin:         "isle",
		Environment:    "local",
		DockerSocket:   config.GetDefaultLocalDockerSocket("/var/run/docker.sock"),
		ProjectName:    projectName,
		ProjectDir:     absProjectDir,
	}, nil
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

func componentDriftSummary(status componentView, limit int) string {
	if status.SDKStatus != nil {
		if summary := summarizeDriftLines(corecomponent.DriftCheckLines(status), limit); summary != "" {
			return summary
		}
	}
	for _, detail := range []string{status.DriftDetail, status.Detail} {
		if detail = strings.TrimSpace(detail); detail != "" {
			return detail
		}
	}
	return "component is drifted"
}

func summarizeDriftLines(lines []string, limit int) string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	if limit <= 0 || len(out) <= limit {
		return strings.Join(out, "; ")
	}
	remaining := len(out) - limit
	return strings.Join(out[:limit], "; ") + fmt.Sprintf("; and %d more", remaining)
}
