package components

import corecomponent "github.com/libops/sitectl/pkg/component"

type RepoAsset = corecomponent.RepoSource
type RuleOp = corecomponent.RuleOp
type YAMLRule = corecomponent.YAMLRule
type YAMLStateSpec = corecomponent.YAMLStateSpec
type DomainSpec = corecomponent.DomainSpec
type Definition = corecomponent.Definition
type Dependencies = corecomponent.Dependencies
type DrupalModuleDependency = corecomponent.DrupalModuleDependency
type DrupalModuleDependencyMode = corecomponent.DrupalModuleDependencyMode
type StateGuidance = corecomponent.StateGuidance

const (
	OpSet         = corecomponent.OpSet
	OpDelete      = corecomponent.OpDelete
	OpRestore     = corecomponent.OpRestore
	OpReplace     = corecomponent.OpReplace
	OpContains    = corecomponent.OpContains
	OpNotContains = corecomponent.OpNotContains

	DrupalModuleDependencyStrict     = corecomponent.DrupalModuleDependencyStrict
	DrupalModuleDependencyEnableOnly = corecomponent.DrupalModuleDependencyEnableOnly
)

type TemplateSource struct {
	Repo string
	Ref  string
}

func (s TemplateSource) ComposeAsset(path string) RepoAsset {
	return RepoAsset{
		Repo: s.Repo,
		Ref:  s.Ref,
		Path: path,
	}
}

func (s TemplateSource) DrupalAsset(path string) RepoAsset {
	return RepoAsset{
		Repo: s.Repo,
		Ref:  s.Ref,
		Path: path,
	}
}

var ParseStateOverrides = corecomponent.ParseStateOverrides

type FollowUpSpec = corecomponent.FollowUpSpec
