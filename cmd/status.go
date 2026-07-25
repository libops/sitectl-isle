package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/helpers"
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
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
	rootfs, err := resolveCodebaseRootfsForContext(cmd, ctx, opts.CodebaseRootfs, opts.DrupalRootfs)
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
	return detectComponentViewsForDefinitions(siteCtx, drupalRootfs, orderedComponentDefinitions()...)
}

func detectComponentViewsForDefinitions(siteCtx *config.Context, drupalRootfs string, defs ...corecomponent.Definition) ([]componentView, error) {
	definitions := componentDefinitions()
	sdkStatuses, err := corecomponent.DetectComponentStatuses(siteCtx, siteCtx.ProjectDir, corecomponent.DetectOptions{
		ComposeRoot:  siteCtx.ProjectDir,
		DrupalRootfs: drupalRootfs,
	}, defs...)
	if err != nil {
		return nil, err
	}

	views := make([]componentView, 0, len(sdkStatuses)+2)
	for i := range sdkStatuses {
		state := sdkStatuses[i].State
		sdkStatus := &sdkStatuses[i]
		followUps := map[string]string{}
		disposition := dispositionFromDetectedState(state)
		switch sdkStatuses[i].Name {
		case "fcrepo":
			if state != corecomponent.DetectedState(corecomponent.StateOff) {
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
		case "codebase":
			disposition = codebaseDisposition(state)
		case coretraefik.BotMitigationName:
			if enabled, known := localBotMitigationConfigured(siteCtx.ProjectDir); known {
				state = corecomponent.DetectedState(corecomponent.StateOff)
				if enabled {
					state = corecomponent.DetectedState(corecomponent.StateOn)
				}
				disposition = dispositionFromDetectedState(state)
				sdkStatus = nil
			}
		case coretraefik.IngressName:
			followUps = currentIngressFollowUps(siteCtx)
		case createpkg.FeatureBundleMergePDF, createpkg.FeatureBundleHOCRSearch:
			followUps = createpkg.FeatureBundleCurrentOptions(siteCtx.ProjectDir, drupalRootfs, siteCtx.EnvFile, sdkStatuses[i].Name)
			if state == corecomponent.DetectedState(corecomponent.StateOn) || state == corecomponent.DetectedState(corecomponent.StateOff) {
				enabled := state == corecomponent.DetectedState(corecomponent.StateOn)
				if err := createpkg.ValidateFeatureBundleObservedState(createpkg.Options{
					Path:                 siteCtx.ProjectDir,
					DrupalRootfs:         drupalRootfs,
					EnvFiles:             append([]string{}, siteCtx.EnvFile...),
					FeatureBundleOptions: map[string]map[string]string{sdkStatuses[i].Name: followUps},
				}, sdkStatuses[i].Name, enabled); err != nil {
					check := &sdkStatuses[i].Off
					if enabled {
						check = &sdkStatuses[i].On
					}
					check.Failed++
					check.Results = append(check.Results, corecomponent.RuleCheckResult{
						Domain: "feature",
						Match:  false,
						Detail: err.Error(),
					})
					sdkStatuses[i].State = corecomponent.StateDrifted
					state = corecomponent.StateDrifted
					disposition = dispositionFromDetectedState(state)
				}
			}
		default:
			if createpkg.IsDerivativeService(sdkStatuses[i].Name) {
				disposition = derivativeServiceDisposition(state)
			}
		}
		views = append(views, componentView{
			Definition:     definitions[sdkStatuses[i].Name],
			Name:           sdkStatuses[i].Name,
			State:          state,
			Disposition:    disposition,
			SDKStatus:      sdkStatus,
			FollowUpValues: followUps,
		})
	}

	return views, nil
}

func currentIngressTrustedIPs(siteCtx *config.Context) string {
	return currentIngressFollowUps(siteCtx)["trusted-ip"]
}

func currentIngressFollowUps(siteCtx *config.Context) map[string]string {
	followUps := map[string]string{}
	if siteCtx == nil {
		return followUps
	}
	compose, err := corecomponent.LoadComposeFileForContext(siteCtx, siteCtx.ResolveProjectPath("docker-compose.yml"))
	if err != nil {
		return followUps
	}
	command := composeServiceCommand(compose, "traefik")
	tlsMode := composeServiceEnvValue(compose, "traefik", "SITECTL_TLS_MODE")
	projectEnv := healthcheck.ProjectEnv(siteCtx)
	followUps["mode"] = detectIngressMode(command, tlsMode)
	if domain := domainFromTraefikRoute(siteCtx, projectEnv); domain != "" {
		followUps["domain"] = domain
	}
	if domain := firstISLEIngressHostname(composeServiceEnvValue(compose, "drupal", "INGRESS_HOSTNAMES")); domain != "" && followUps["domain"] == "" {
		followUps["domain"] = domain
	}
	if domain := domainFromServiceURL(composeServiceEnvValue(compose, "drupal", "DRUPAL_DEFAULT_SITE_URL"), projectEnv); domain != "" && followUps["domain"] == "" {
		followUps["domain"] = domain
	}
	if followUps["domain"] == "" {
		followUps["domain"] = strings.TrimSpace(projectEnv["DOMAIN"])
	}
	if email := helpers.FirstNonEmpty(
		commandValueByPrefix(command, "--certificatesResolvers.letsencrypt.acme.email="),
		commandValueByPrefix(command, "--certificatesresolvers.letsencrypt.acme.email="),
	); email != "" {
		followUps["acme-email"] = email
	}
	if trustedIPs := helpers.FirstNonEmpty(
		commandValueByPrefix(command, "--entryPoints.http.forwardedHeaders.trustedIPs="),
		commandValueByPrefix(command, "--entrypoints.http.forwardedHeaders.trustedIPs="),
	); trustedIPs != "" {
		followUps["trusted-ip"] = trustedIPs
	}
	for _, item := range []struct {
		key string
		env string
	}{
		{key: "max-upload-size", env: "NGINX_CLIENT_MAX_BODY_SIZE"},
		{key: "upload-timeout", env: "NGINX_CLIENT_BODY_TIMEOUT"},
	} {
		if value := composeServiceEnvValue(compose, "drupal", item.env); value != "" {
			followUps[item.key] = value
		}
	}
	return followUps
}

func domainFromTraefikRoute(siteCtx *config.Context, projectEnv map[string]string) string {
	publicURL, ok, err := healthcheck.PublicURLFromTraefik(siteCtx, healthcheck.TraefikRouteOptions{
		AppService:    "drupal",
		Router:        "drupal",
		DefaultScheme: "http",
		DefaultDomain: coretraefik.DefaultIngressDomain,
	})
	if err != nil || !ok {
		return ""
	}
	return domainFromServiceURL(publicURL, projectEnv)
}

func detectIngressMode(command, tlsMode string) string {
	if mode, ok := coretraefik.NormalizeIngressMode(tlsMode); ok && mode != coretraefik.IngressModeHTTP {
		return mode
	}
	if commandValueByPrefix(command, "--certificatesResolvers.letsencrypt.acme.email=") != "" ||
		commandValueByPrefix(command, "--certificatesresolvers.letsencrypt.acme.email=") != "" {
		return coretraefik.IngressModeHTTPSLetsEncrypt
	}
	for _, line := range composeStringLines(command) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--entryPoints.https.address=") || strings.HasPrefix(line, "--entrypoints.https.address=") {
			return coretraefik.IngressModeHTTPSCustom
		}
	}
	return coretraefik.IngressModeHTTP
}

func domainFromServiceURL(value string, env map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return ""
	}
	host := os.Expand(strings.TrimSpace(parsed.Host), func(name string) string {
		return strings.TrimSpace(env[name])
	})
	if strings.Contains(host, "$") {
		return ""
	}
	return strings.TrimSpace(host)
}

func commandValueByPrefix(command, prefix string) string {
	for _, line := range composeStringLines(command) {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func composeStringLines(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == ' '
	})
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

func codebaseDisposition(state corecomponent.DetectedState) corecomponent.Disposition {
	switch state {
	case corecomponent.DetectedState(corecomponent.StateOn):
		return corecomponent.DispositionGitRoot
	case corecomponent.DetectedState(corecomponent.StateOff):
		return corecomponent.DispositionNested
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

func composeServiceCommand(compose *corecomponent.ComposeFile, service string) string {
	if compose == nil {
		return ""
	}
	block, ok := compose.ServiceBlock(service)
	if !ok {
		return ""
	}
	lines := strings.Split(block, "\n")
	commandIdx, ok := findComposeMapKey(lines, 1, "command", 4)
	if !ok {
		return ""
	}
	line := strings.TrimSpace(lines[commandIdx])
	if strings.HasPrefix(line, "command: ") && !strings.HasSuffix(line, "|") && !strings.HasSuffix(line, ">") && !strings.HasSuffix(line, "|-") && !strings.HasSuffix(line, ">-") {
		return strings.TrimSpace(strings.TrimPrefix(line, "command: "))
	}
	end := findComposeBlockEnd(lines, commandIdx, 4)
	commandLines := make([]string, 0, end-commandIdx-1)
	for i := commandIdx + 1; i < end; i++ {
		commandLines = append(commandLines, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "- ")))
	}
	return strings.Join(commandLines, "\n")
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

func findComposeMapKey(lines []string, start int, key string, indent int) (int, bool) {
	prefix := strings.Repeat(" ", indent) + key + ":"
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		currentIndent := leadingSpaces(line)
		if currentIndent < indent {
			break
		}
		if currentIndent == indent && strings.HasPrefix(line, prefix) {
			return i, true
		}
	}
	return 0, false
}

func findComposeBlockEnd(lines []string, start int, indent int) int {
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leadingSpaces(line) <= indent {
			return i
		}
	}
	return len(lines)
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
		DockerHostType:     config.ContextLocal,
		Name:               projectName,
		Site:               projectName,
		Plugin:             "isle",
		Environment:        "local",
		DockerSocket:       config.GetDefaultLocalDockerSocket("/var/run/docker.sock"),
		ComposeProjectName: projectName,
		ProjectDir:         absProjectDir,
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
