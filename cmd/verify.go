package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/plugin"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

const (
	verifyExpectedAuto = "auto"
	verifyIIIFLocal    = "local"
)

type isleVerifyRunner struct {
	path              string
	fcrepo            string
	blazegraph        string
	iiif              string
	iiifTopology      string
	botMitigation     string
	isleFileSystemURI string
	demoObjects       bool
}

func (r *isleVerifyRunner) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&r.path, "path", "", "Project path override")
	cmd.Flags().StringVar(&r.fcrepo, "fcrepo", verifyExpectedAuto, "Expected Fcrepo state: auto, on, or off")
	cmd.Flags().StringVar(&r.blazegraph, "blazegraph", verifyExpectedAuto, "Expected Blazegraph state: auto, on, or off")
	cmd.Flags().StringVar(&r.iiif, "iiif", verifyExpectedAuto, "Expected IIIF implementation: auto, triplet, or cantaloupe")
	cmd.Flags().StringVar(&r.iiifTopology, "iiif-topology", verifyExpectedAuto, "Expected IIIF topology: auto, disabled/local, or distributed/external")
	cmd.Flags().StringVar(&r.botMitigation, "bot-mitigation", verifyExpectedAuto, "Expected bot mitigation state: auto, on, or off")
	cmd.Flags().StringVar(&r.isleFileSystemURI, "isle-file-system-uri", verifyExpectedAuto, "Expected ISLE filesystem URI when Fcrepo is off: auto, public, private, or a custom URI")
	cmd.Flags().BoolVar(&r.demoObjects, "demo-objects", false, "Create demo objects and verify repository content grows. Intended for CI or disposable non-production sites.")
}

func (r *isleVerifyRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	verifyCtx, err := resolveVerifyContext(ctx, r.path)
	if err != nil {
		return nil, err
	}

	checker, err := healthcheck.NewDockerChecker(verifyCtx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = checker.Close() }()

	projectDir := strings.TrimSpace(verifyCtx.ProjectDir)
	results := []sitevalidate.Result{}

	fcrepoExpected, result := resolveExpectedOnOff(cmd.Context(), checker, "fcrepo", r.fcrepo)
	results = append(results, result)
	blazegraphExpected, result := resolveExpectedOnOff(cmd.Context(), checker, "blazegraph", r.blazegraph)
	results = append(results, result)

	results = append(results, verifyServiceExpected(cmd.Context(), checker, "fcrepo", fcrepoExpected))
	results = append(results, verifyServiceExpected(cmd.Context(), checker, "blazegraph", blazegraphExpected))
	results = append(results, verifyIIIF(cmd.Context(), checker, projectDir, r.iiif, r.iiifTopology)...)
	results = append(results, verifyBotMitigation(cmd.Context(), verifyCtx, projectDir, r.botMitigation)...)

	if fcrepoExpected == createpkg.FcrepoStateOff {
		results = append(results, verifyNoFedoraManagedFiles(cmd.Context(), projectDir))
	}

	if r.demoObjects {
		results = append(results, verifyDemoObjects(cmd.Context(), projectDir, fcrepoExpected, r.isleFileSystemURI))
	}

	return results, nil
}

var _ plugin.VerifyRunner = (*isleVerifyRunner)(nil)

func resolveVerifyContext(ctx *config.Context, path string) (*config.Context, error) {
	if strings.TrimSpace(path) != "" {
		return localStatusContext(path)
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	if strings.TrimSpace(ctx.ProjectDir) == "" {
		return nil, fmt.Errorf("context %q does not define a project directory; pass --path or update the sitectl context", ctx.Name)
	}
	return ctx, nil
}

func resolveExpectedOnOff(ctx context.Context, checker *healthcheck.DockerChecker, service, expected string) (string, sitevalidate.Result) {
	expected = strings.ToLower(strings.TrimSpace(expected))
	switch expected {
	case "", verifyExpectedAuto:
		exists, err := checker.ServiceExists(ctx, service)
		if err != nil {
			return verifyExpectedAuto, failedVerifyResult("verify:"+service+":expected", fmt.Sprintf("detect service %q: %v", service, err), "")
		}
		if exists {
			return createpkg.FcrepoStateOn, okVerifyResult("verify:"+service+":expected", "detected service")
		}
		return createpkg.FcrepoStateOff, okVerifyResult("verify:"+service+":expected", "service absent")
	case createpkg.FcrepoStateOn, createpkg.FcrepoStateOff:
		return expected, okVerifyResult("verify:"+service+":expected", expected)
	default:
		return expected, failedVerifyResult("verify:"+service+":expected", fmt.Sprintf("invalid expected state %q", expected), "use auto, on, or off")
	}
}

func verifyServiceExpected(ctx context.Context, checker *healthcheck.DockerChecker, service, expected string) sitevalidate.Result {
	name := "verify:service:" + service
	if expected != createpkg.FcrepoStateOn && expected != createpkg.FcrepoStateOff {
		return failedVerifyResult(name, "expected state could not be resolved", "use auto, on, or off")
	}
	exists, err := checker.ServiceExists(ctx, service)
	if err != nil {
		return failedVerifyResult(name, err.Error(), "")
	}
	switch {
	case expected == createpkg.FcrepoStateOn && exists:
		return okVerifyResult(name, "service is present")
	case expected == createpkg.FcrepoStateOff && !exists:
		return okVerifyResult(name, "service is absent")
	case expected == createpkg.FcrepoStateOn:
		return failedVerifyResult(name, "service is absent", "re-run sitectl create with --"+service+" on")
	default:
		return failedVerifyResult(name, "service is present", "re-run sitectl create with --"+service+" off")
	}
}

func verifyIIIF(ctx context.Context, checker *healthcheck.DockerChecker, projectDir, expectedImplementation, expectedTopology string) []sitevalidate.Result {
	results := []sitevalidate.Result{}
	expectedImplementation = strings.ToLower(strings.TrimSpace(expectedImplementation))
	expectedTopology = normalizeVerifyIIIFTopology(expectedTopology)

	tripletExists, tripletErr := checker.ServiceExists(ctx, "triplet")
	cantaloupeExists, cantaloupeErr := checker.ServiceExists(ctx, "cantaloupe")
	if tripletErr != nil {
		results = append(results, failedVerifyResult("verify:iiif:triplet-service", tripletErr.Error(), ""))
	}
	if cantaloupeErr != nil {
		results = append(results, failedVerifyResult("verify:iiif:cantaloupe-service", cantaloupeErr.Error(), ""))
	}
	if tripletErr != nil || cantaloupeErr != nil {
		return results
	}

	if expectedImplementation == "" || expectedImplementation == verifyExpectedAuto {
		switch {
		case tripletExists:
			expectedImplementation = createpkg.IIIFTriplet
		case cantaloupeExists:
			expectedImplementation = createpkg.IIIFCantaloupe
		default:
			results = append(results, failedVerifyResult("verify:iiif:implementation", "no local IIIF service detected", "expected triplet or cantaloupe"))
			return results
		}
	} else if expectedImplementation != createpkg.IIIFTriplet && expectedImplementation != createpkg.IIIFCantaloupe {
		results = append(results, failedVerifyResult("verify:iiif:implementation", fmt.Sprintf("invalid expected implementation %q", expectedImplementation), "use auto, triplet, or cantaloupe"))
		return results
	}

	switch expectedImplementation {
	case createpkg.IIIFTriplet:
		results = append(results, verifyBool("verify:iiif:triplet-service", tripletExists, "triplet service is present", "triplet service is absent", "re-run sitectl create with --iiif triplet"))
		results = append(results, verifyBool("verify:iiif:cantaloupe-service", !cantaloupeExists, "cantaloupe service is absent", "cantaloupe service is present", "re-run sitectl create with --iiif triplet"))
		if expectedTopology == verifyIIIFLocal {
			results = append(results, verifyProjectFileContains(projectDir, "docker-compose.yml", `DRUPAL_DEFAULT_CANTALOUPE_URL: "http://localhost/iiif/3"`, "verify:iiif:drupal-url"))
			results = append(results, verifyProjectFileExists(projectDir, filepath.Join("conf", "triplet", "config.yaml"), "verify:iiif:triplet-config"))
		}
	case createpkg.IIIFCantaloupe:
		results = append(results, verifyBool("verify:iiif:cantaloupe-service", cantaloupeExists, "cantaloupe service is present", "cantaloupe service is absent", "re-run sitectl create with --iiif cantaloupe"))
		results = append(results, verifyBool("verify:iiif:triplet-service", !tripletExists, "triplet service is absent", "triplet service is present", "re-run sitectl create with --iiif cantaloupe"))
	}

	return results
}

func normalizeVerifyIIIFTopology(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", verifyExpectedAuto, "disabled", verifyIIIFLocal:
		return verifyIIIFLocal
	case "distributed", createpkg.IIIFTopologyExternal:
		return createpkg.IIIFTopologyExternal
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func verifyBotMitigation(ctx context.Context, verifyCtx *config.Context, projectDir, expected string) []sitevalidate.Result {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" || expected == verifyExpectedAuto {
		enabled, known := localBotMitigationConfigured(projectDir)
		if !known {
			return []sitevalidate.Result{warningVerifyResult("verify:bot-mitigation:expected", "could not infer bot mitigation state without a local docker-compose.yml")}
		}
		if enabled {
			expected = coretraefik.BotMitigationStateOn
		} else {
			expected = coretraefik.BotMitigationStateOff
		}
	}
	switch expected {
	case coretraefik.BotMitigationStateOff:
		return []sitevalidate.Result{okVerifyResult("verify:bot-mitigation", "expected off")}
	case coretraefik.BotMitigationStateOn:
		if !botMitigationForwardedHeaderProbeEnabled(verifyCtx) {
			return []sitevalidate.Result{okVerifyResult("verify:bot-mitigation", "configured; skipped X-Forwarded-For challenge probe because ingress trusted IPs are not configured")}
		}
		return []sitevalidate.Result{checkBotMitigationChallenge(ctx, verifyCtx)}
	default:
		return []sitevalidate.Result{failedVerifyResult("verify:bot-mitigation:expected", fmt.Sprintf("invalid expected state %q", expected), "use auto, on, or off")}
	}
}

func botMitigationForwardedHeaderProbeEnabled(verifyCtx *config.Context) bool {
	return strings.TrimSpace(currentIngressTrustedIPs(verifyCtx)) != ""
}

func localBotMitigationConfigured(projectDir string) (bool, bool) {
	if strings.TrimSpace(projectDir) == "" {
		return false, false
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml")) // #nosec G304 -- projectDir is the selected site checkout.
	if err != nil {
		return false, false
	}
	text := string(data)
	return strings.Contains(text, "captcha-protect") || strings.Contains(text, "challenge.tmpl.html"), true
}

func checkBotMitigationChallenge(ctx context.Context, verifyCtx *config.Context) sitevalidate.Result {
	target := healthcheck.PublicURLFromEnv(verifyCtx, "http", "islandora.io")
	client, err := botMitigationHTTPClient(target, verifyCtx)
	if err != nil {
		return failedVerifyResult("verify:bot-mitigation", err.Error(), "")
	}
	var lastStatus string
	var lastBody string
	for i := 0; i < 24; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return failedVerifyResult("verify:bot-mitigation", err.Error(), "")
		}
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			lastStatus = resp.Status
			lastBody = string(body)
			if resp.StatusCode == http.StatusTooManyRequests && strings.Contains(lastBody, "Verifying connection") {
				return okVerifyResult("verify:bot-mitigation", resp.Status)
			}
		} else {
			lastStatus = err.Error()
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failedVerifyResult("verify:bot-mitigation", ctx.Err().Error(), "")
		case <-timer.C:
		}
	}
	detail := "expected 429 challenge for X-Forwarded-For: 1.2.3.4, got " + lastStatus
	if strings.TrimSpace(lastBody) != "" {
		detail += "; body=" + strings.TrimSpace(firstN(lastBody, 300))
	}
	return failedVerifyResult("verify:bot-mitigation", detail, "check captcha-protect middleware and Traefik routing")
}

func botMitigationHTTPClient(target string, verifyCtx *config.Context) (*http.Client, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse bot mitigation target %q: %w", target, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if verifyCtx != nil && verifyCtx.DockerHostType == config.ContextLocal {
		host := parsed.Hostname()
		port := parsed.Port()
		if port == "" {
			if parsed.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		targetAddr := net.JoinHostPort(host, port)
		localAddr := net.JoinHostPort("127.0.0.1", port)
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == targetAddr {
				addr = localAddr
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}, nil
}

func verifyNoFedoraManagedFiles(ctx context.Context, projectDir string) sitevalidate.Result {
	if strings.TrimSpace(projectDir) == "" {
		return warningVerifyResult("verify:fcrepo:file-managed", "skipped because the context does not define a local project directory")
	}
	out, err := runLocalProjectOutput(ctx, projectDir, "docker", "compose", "exec", "-T", "drupal", "bash", "-lc", `drush --root=/var/www/drupal sql:query --extra=--skip-column-names "SELECT COUNT(*) FROM file_managed WHERE uri LIKE 'fedora%';"`)
	if err != nil {
		return failedVerifyResult("verify:fcrepo:file-managed", err.Error(), "")
	}
	count := strings.TrimSpace(out)
	if count == "" {
		count = "0"
	}
	if count == "0" {
		return okVerifyResult("verify:fcrepo:file-managed", "no fedora-backed file_managed URIs")
	}
	return failedVerifyResult("verify:fcrepo:file-managed", "expected 0 fedora-backed file_managed URIs, got "+count, "re-run sitectl create with --fcrepo off")
}

func verifyDemoObjects(ctx context.Context, projectDir, fcrepoExpected, fileSystemURI string) sitevalidate.Result {
	if strings.TrimSpace(projectDir) == "" {
		return warningVerifyResult("verify:demo-objects", "skipped because the context does not define a local project directory")
	}
	service, target := demoObjectAssertTarget(fcrepoExpected, fileSystemURI)
	before, err := countContainerFiles(ctx, projectDir, service, target)
	if err != nil {
		return failedVerifyResult("verify:demo-objects", err.Error(), "")
	}
	if _, err := runLocalProjectOutput(ctx, projectDir, "make", "demo-objects"); err != nil {
		return failedVerifyResult("verify:demo-objects", err.Error(), "")
	}
	for i := 0; i < 24; i++ {
		after, err := countContainerFiles(ctx, projectDir, service, target)
		if err == nil && after > before {
			return okVerifyResult("verify:demo-objects", fmt.Sprintf("%s:%s grew from %d to %d files", service, target, before, after))
		}
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failedVerifyResult("verify:demo-objects", ctx.Err().Error(), "")
		case <-timer.C:
		}
	}
	return failedVerifyResult("verify:demo-objects", fmt.Sprintf("expected ingested content to appear in %s:%s", service, target), "check Islandora ingest workers and queue consumers")
}

func demoObjectAssertTarget(fcrepoExpected, fileSystemURI string) (string, string) {
	if fcrepoExpected == createpkg.FcrepoStateOn {
		return "fcrepo", "/data"
	}
	switch strings.ToLower(strings.TrimSpace(fileSystemURI)) {
	case createpkg.PublicISLEFileSystemURI:
		return "drupal", "/var/www/drupal/web/sites/default/files"
	default:
		return "drupal", "/var/www/drupal/private"
	}
}

func countContainerFiles(ctx context.Context, projectDir, service, target string) (int, error) {
	out, err := runLocalProjectOutput(ctx, projectDir, "docker", "compose", "exec", "-T", service, "bash", "-lc", "find "+strconv.Quote(target)+" -type f 2>/dev/null | wc -l")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse file count %q: %w", strings.TrimSpace(out), err)
	}
	return count, nil
}

func verifyProjectFileContains(projectDir, relPath, expected, name string) sitevalidate.Result {
	if strings.TrimSpace(projectDir) == "" {
		return warningVerifyResult(name, "skipped because the context does not define a local project directory")
	}
	data, err := os.ReadFile(filepath.Join(projectDir, relPath)) // #nosec G304 -- projectDir is the selected site checkout.
	if err != nil {
		return failedVerifyResult(name, err.Error(), "")
	}
	if strings.Contains(string(data), expected) {
		return okVerifyResult(name, relPath)
	}
	return failedVerifyResult(name, relPath+" does not contain expected value", "re-run sitectl create with the expected IIIF options")
}

func verifyProjectFileExists(projectDir, relPath, name string) sitevalidate.Result {
	if strings.TrimSpace(projectDir) == "" {
		return warningVerifyResult(name, "skipped because the context does not define a local project directory")
	}
	if _, err := os.Stat(filepath.Join(projectDir, relPath)); err != nil {
		return failedVerifyResult(name, err.Error(), "")
	}
	return okVerifyResult(name, relPath)
}

func runLocalProjectOutput(ctx context.Context, projectDir, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...) // #nosec G204 -- verify runs fixed commands assembled by sitectl-isle.
	command.Dir = projectDir
	command.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), detail)
	}
	return stdout.String(), nil
}

func verifyBool(name string, ok bool, okDetail, failedDetail, fixHint string) sitevalidate.Result {
	if ok {
		return okVerifyResult(name, okDetail)
	}
	return failedVerifyResult(name, failedDetail, fixHint)
}

func okVerifyResult(name, detail string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusOK, Detail: detail}
}

func warningVerifyResult(name, detail string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusWarning, Detail: detail}
}

func failedVerifyResult(name, detail, fixHint string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusFailed, Detail: detail, FixHint: fixHint}
}

func firstN(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
