package cmd

// IncludedPlugins returns plugin dependencies that should be installed and
// accepted by ISLE contexts.
func IncludedPlugins() []string {
	return []string{"drupal"}
}
