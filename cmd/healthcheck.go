package cmd

import (
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

	drupalURL := healthcheck.PublicURLFromEnv(ctx, "http", "islandora.io")
	if value, ok, err := checker.ServiceEnv(cmd.Context(), "drupal", "DRUPAL_DEFAULT_SITE_URL"); err == nil && ok && strings.TrimSpace(value) != "" {
		drupalURL = value
	}

	results := []sitevalidate.Result{
		checker.CheckHTTPRoute(
			cmd.Context(),
			"http:drupal",
			"drupal",
			drupalURL,
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
