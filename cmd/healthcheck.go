package cmd

import (
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
		checker.CheckHTTPRoute(
			cmd.Context(),
			"http:drupal",
			"drupal",
			isleDrupalPublicURL(ctx),
		),
	}

	results = append(results,
		checker.CheckMariaDB(cmd.Context(), "mariadb"),
		checker.CheckComposeServiceDependsOnHealthy(cmd.Context(), "drupal", "mariadb"),
		checker.CheckSolrCore(cmd.Context(), "solr", "default"),
	)

	optionalResults, err := checker.CheckOptionalHTTPServices(cmd.Context(),
		healthcheck.OptionalHTTPServiceCheck{Service: "homarus", Name: "http:homarus-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
		healthcheck.OptionalHTTPServiceCheck{Service: "houdini", Name: "http:houdini-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
		healthcheck.OptionalHTTPServiceCheck{Service: "hypercube", Name: "http:hypercube-healthcheck", URL: "http://127.0.0.1:8080/healthcheck"},
	)
	if err != nil {
		return nil, err
	}
	results = append(results, optionalResults...)

	return results, nil
}

var _ plugin.HealthcheckRunner = isleHealthcheckRunner{}

func isleDrupalPublicURL(ctx *config.Context) string {
	target := healthcheck.PublicURLFromEnv(ctx, "http", "localhost")
	if traefikURL, ok, err := healthcheck.PublicURLFromTraefik(ctx, healthcheck.TraefikRouteOptions{
		AppService:    "drupal",
		Router:        "drupal",
		DefaultScheme: "http",
		DefaultDomain: "localhost",
	}); err == nil && ok {
		target = traefikURL
	}
	return target
}
