package cmd

import (
	"context"
	"net/url"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

type isleHealthcheckRunner struct{}

func (isleHealthcheckRunner) BindFlags(cmd *cobra.Command) {}

func (isleHealthcheckRunner) Run(cmd *cobra.Command, ctx *config.Context) ([]sitevalidate.Result, error) {
	checker, err := healthcheck.NewDockerChecker(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = checker.Close() }()

	results := []sitevalidate.Result{
		checker.CheckHTTPFromContainerWithHostHeader(
			cmd.Context(),
			"http:drupal",
			"drupal",
			"http://127.0.0.1/",
			publicURLHost(healthcheck.PublicURLFromEnv(ctx, "http", "islandora.io")),
		),
	}

	results = append(results,
		checker.CheckMariaDB(cmd.Context(), "mariadb"),
		checker.CheckSolrCore(cmd.Context(), "solr", "default"),
	)

	optionalChecks := []struct {
		service string
		name    string
		url     string
	}{
		{service: "homarus", name: "http:homarus-healthcheck", url: "http://127.0.0.1:8080/healthcheck"},
		{service: "houdini", name: "http:houdini-healthcheck", url: "http://127.0.0.1:8080/healthcheck"},
		{service: "hypercube", name: "http:hypercube-healthcheck", url: "http://127.0.0.1:8080/healthcheck"},
	}
	for _, check := range optionalChecks {
		ready, err := optionalHTTPServiceReady(cmd.Context(), checker, check.service)
		if err != nil {
			return nil, err
		}
		if !ready {
			continue
		}
		results = append(results, checker.CheckHTTPFromContainer(cmd.Context(), check.name, check.service, check.url))
	}

	return results, nil
}

var _ plugin.HealthcheckRunner = isleHealthcheckRunner{}

func optionalHTTPServiceReady(ctx context.Context, checker *healthcheck.DockerChecker, service string) (bool, error) {
	exists, err := checker.ServiceExists(ctx, service)
	if err != nil || !exists {
		return false, err
	}
	results, err := checker.CheckComposeServices(ctx, service)
	if err != nil {
		return false, err
	}
	for _, result := range results {
		if result.Status != sitevalidate.StatusOK {
			return false, nil
		}
	}
	return len(results) > 0, nil
}

func publicURLHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "islandora.io"
	}
	if host := parsed.Hostname(); host != "" {
		return host
	}
	return "islandora.io"
}
