package main

import (
	"cmp"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/mesh"
)

const reposUsage = "usage: bmesh repos [--json] | bmesh repos show <name> [--json] | repos publish | repos propose [--wait D] | " +
	"repos remove <name> [--checkouts [--ignored-ok]] [--apply] [--wait D]"

// cmdRepos prints the registry (repos.toml): the table, or one row; or it
// publishes, proposes or removes rows of the admin's registry (023).
func (a *app) cmdRepos(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "publish":
			return a.cmdReposPublish(args[1:])
		case "propose":
			return a.cmdReposPropose(args[1:])
		case "remove":
			return a.cmdReposRemove(args[1:])
		}
	}
	fs := a.flags("repos")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if err := a.loadConfig(); err != nil {
		return err
	}
	switch {
	case len(pos) == 0:
		return a.reposTable()
	case len(pos) == 2 && pos[0] == "show":
		r, ok := a.cfg.Repos[pos[1]]
		if !ok {
			return fail(exitRefused, "no repo %q in %s (bmesh repos lists them)", pos[1], config.ReposPath(a.cfg.Path()))
		}
		if a.json {
			return a.printJSON(r)
		}
		for _, kv := range [][2]string{{"name", r.Name}, {"path", r.Path}, {"mode", r.Mode}, {"remote", r.Remote},
			{"host", r.Host}, {"ux_host", r.UXHost}, {"verify", strings.Join(r.Verify, "; ")},
			{"surfaces", strings.Join(r.Surfaces, ", ")}, {"pin", r.Pin}, {"routines", strings.Join(r.Routines, ", ")},
			{"code", cmp.Or(strings.Join(r.Code, ", "), "(everything but the spec files)")}} {
			fmt.Fprintf(a.stdout, "%s: %s\n", kv[0], kv[1])
		}
		return nil
	}
	return fail(exitUsage, reposUsage)
}

// reposTable prints every row, sorted by name: as a table, or with --json as
// an array of rows.
func (a *app) reposTable() error {
	rows := make([]config.Repo, 0, len(a.cfg.Repos))
	for _, n := range a.cfg.RepoNames() {
		rows = append(rows, a.cfg.Repos[n])
	}
	if a.json {
		return a.printJSON(rows)
	}
	w := tabwriter.NewWriter(a.stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tMODE\tREMOTE\tVERIFY\tHOST\tUX_HOST\tSURFACES\tPIN")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Mode, r.Remote, strings.Join(r.Verify, "; "),
			r.Host, r.UXHost, strings.Join(r.Surfaces, ", "), r.Pin)
	}
	return w.Flush()
}

// tokenClash refuses (exit 3) a registry in which two names map to one
// path token (mesh.RepoToken: Foo and foo, my.app and my_app): their
// rounds, leads and families would be one node path.
func tokenClash(rows map[string]config.Repo) error {
	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)
	seen := map[string]string{}
	for _, n := range names {
		t := mesh.RepoToken(n)
		if other, ok := seen[t]; ok {
			return fail(exitRefused, "repos %s and %s are both %s in node paths; rename one", other, n, t)
		}
		seen[t] = n
	}
	return nil
}
