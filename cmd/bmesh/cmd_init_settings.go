package main

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strings"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// setting is one line of init's "What this box is set to:" block: the
// value in effect, what it means for someone new to buildermesh, and the
// key (or command) that changes it.
type setting struct{ name, value, why, keys string }

func (s setting) String() string {
	return fmt.Sprintf("%s: %s. %s (%s)", s.name, s.value, s.why, s.keys)
}

// Explanations shared by the block and the comments init writes into
// config.toml.
const (
	whyEmbedded = "bmesh serve runs the message hub itself; nothing to install"
	whyHub      = "The message hub every box's nodes talk through"
	whyToken    = "The hub's token is read from this file"
)

// whyRunner is what a runner kind means.
func whyRunner(kind string) string {
	switch kind {
	case "claude":
		return "Each lead and proposal round is a claude session started for you"
	case "t3":
		return "Each lead and proposal round is a thread in this box's T3 Code"
	case "tmux":
		return "Each lead and proposal round is claude in a tmux window"
	}
	return "The program that runs each lead and proposal round"
}

// settings is the block for c, read back from the loaded config so the
// defaults init didn't write show too. service is the service file that
// keeps bmesh serve running, "" for none.
func settings(c *config.Config, service string) []setting {
	var out []setting
	hub, embedded := c.HubURL()
	if embedded {
		out = append(out,
			setting{"setup", "single box", "This box runs everything; `bmesh init --force --hub <url>` joins a hub other boxes share instead", "[mesh] nats"},
			setting{"hub", "embedded on " + strings.TrimPrefix(hub, "nats://"), whyEmbedded, "[mesh] nats, listen"})
	} else {
		token := "no token"
		if c.Mesh.TokenFile != "" {
			token = "token from " + c.Mesh.TokenFile
		}
		out = append(out,
			setting{"setup", "several boxes", "This box shares a hub with other boxes (README: Several boxes)", "[mesh] nats"},
			setting{"hub", redact(hub) + ", " + token, whyHub, "[mesh] nats, token_file"})
	}
	out = append(out, setting{"runner", c.Runner.Kind, whyRunner(c.Runner.Kind), "[runner] kind"})
	names := c.RepoNames()
	if len(names) == 0 {
		out = append(out, setting{"repo", "none", "Run bmesh init in a repo's checkout to register it", config.ReposFile})
	}
	for _, n := range names {
		_, p, _ := c.RepoPath(n)
		out = append(out, setting{"repo", n + " at " + p, "Registered: bmesh ideate " + n + " proposes changes to it, bmesh build " + n + " <NNN> builds one", config.ReposFile})
	}
	wt := "Where bmesh makes the separate checkouts its nodes work in, so yours is never touched"
	if c.Runner.Kind == "t3" {
		wt += " (T3 keeps its own threads' checkouts)"
	}
	out = append(out, setting{"worktrees", c.Expand(c.Runner.Worktrees), wt, "[runner] worktrees"})
	out = append(out, setting{"map", "http://" + loopback(c.Serve.Listen) + "/", "A page showing every running node; open it while bmesh serve runs", "[serve] listen"})
	out = append(out,
		setting{"proposers", modelAt(c.Models.Proposer, c.Models.ProposerEffort), "The two sessions of a proposal round, which read the repo and write specs", "[models] proposer, proposer_effort"},
		setting{"leads", modelAt(c.Models.Lead, c.Models.LeadEffort) + ", unless the spec's judge names another", "The session that builds one spec and lands it on main", "[models] lead, lead_effort"},
		setting{"executors", "chosen by each lead", "Helpers a lead starts for pieces of its spec", "bmesh executor new --model"})
	every := config.FormatDuration(c.Update.CheckEvery)
	switch {
	case c.Update.Repo != "":
		out = append(out, setting{"updates", "from " + c.Update.Repo, "The daemon rebuilds bmesh from " + c.Update.Repo + "'s main whenever it moves", "[update] repo"})
	case c.Update.CheckEvery <= 0:
		out = append(out, setting{"updates", "off", "The daemon doesn't check for a newer bmesh; bmesh update installs the newest main", "[update] check_every, upstream, repo"})
	case c.Update.Upstream:
		out = append(out, setting{"updates", "from upstream every " + every, "The daemon installs bmesh's newest main (go install) whenever it moves", "[update] upstream, check_every"})
	default:
		out = append(out, setting{"updates", "notice every " + every, "The daemon checks bmesh's main and each session says when this binary is behind; bmesh update takes it", "[update] check_every, upstream, repo"})
	}
	if service == "" {
		out = append(out, setting{"service", "none", "Run bmesh serve yourself and keep it running, or have the OS do it", "bmesh init --service"})
	} else {
		out = append(out, setting{"service", service, "Keeps bmesh serve running, and starts it again after a reboot", "bmesh init --service"})
	}
	if c.Runner.Checkouts == "" {
		out = append(out, setting{"routines", "off", "This box runs no routine jobs (verify, review); another box on the mesh must", "[runner] checkouts"})
	} else {
		out = append(out, setting{"routines", "on", "This box runs any pushed commit's routine commands for every box on the mesh", "[runner] checkouts"})
	}
	return out
}

// printSettings prints the block.
func printSettings(w io.Writer, c *config.Config, service string) {
	fmt.Fprintln(w, "What this box is set to:")
	for _, s := range settings(c, service) {
		fmt.Fprintf(w, "  %s\n", s)
	}
}

// modelAt is "<model> at <effort> effort", or the model alone when the
// runner picks the effort.
func modelAt(model, effort string) string {
	if effort == "" {
		return model + " at the runner's default effort"
	}
	return model + " at " + effort + " effort"
}

// loopback is addr with an unspecified host (0.0.0.0, ::, "") as
// 127.0.0.1, the address a browser on this box opens.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// redact hides a password in a hub URL.
func redact(u string) string {
	if p, err := url.Parse(u); err == nil && p.User != nil {
		return p.Redacted()
	}
	return u
}

// installedService is the service file init --service writes on this OS
// when it exists, else "".
func installedService() string {
	p, err := serviceFile()
	if err != nil || p == "" {
		return ""
	}
	if _, ok := statOK(p); !ok {
		return ""
	}
	return p
}

// hasMain reports whether the repo at root has a main branch: on origin as
// last fetched when origin has been fetched, else locally (a branch, or an
// unborn HEAD named main). It never reaches the network.
func hasMain(root string) bool {
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	if remote, _ := git("for-each-ref", "--format=%(refname)", "refs/remotes/origin/"); remote != "" {
		for _, r := range strings.Split(remote, "\n") {
			if r == "refs/remotes/origin/main" {
				return true
			}
		}
		return false
	}
	if _, err := git("rev-parse", "--verify", "-q", "refs/heads/main"); err == nil {
		return true
	}
	head, _ := git("symbolic-ref", "-q", "HEAD")
	return head == "refs/heads/main" // unborn: the first commit will be main's
}

// errNoMain is init's refusal of a repo without main.
func errNoMain(path string) error {
	return fail(exitRefused, "%s has no main branch; bmesh builds on main (rename the branch, or see https://github.com/rickdiculousli/buildermesh/issues/1; --no-repo sets up without it)", path)
}
