// Package buildermesh embeds the files of this repo that the bmesh binary
// hands to other repos: the paragraph a repo's AGENTS.md gets, the README of
// a proposals directory and the example routine kinds. They stay in the repo
// for reading and are embedded from there, so the two never differ.
package buildermesh

import "embed"

// AgentsSnippet is docs/agents-snippet.md: the buildermesh paragraph for a
// repo's AGENTS.md, with its bracketed verify placeholder.
//
//go:embed docs/agents-snippet.md
var AgentsSnippet string

// ProposalsReadme is docs/proposals/README.md: what a proposals directory
// holds and the lifecycle of a spec.
//
//go:embed docs/proposals/README.md
var ProposalsReadme string

// Examples holds examples/routines/<kind>.toml, the kinds a repo is seeded
// with.
//
//go:embed examples/routines/*.toml
var Examples embed.FS
