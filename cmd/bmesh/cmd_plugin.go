package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rickdiculousli/buildermesh/internal/config"
	"github.com/rickdiculousli/buildermesh/internal/plugin"
)

const pluginUsage = "usage: bmesh plugin [--install]"

// cmdPlugin says where the plugin this binary carries is written, which
// binary wrote it and whether Claude Code reads it; --install writes it and
// points Claude Code at it.
func (a *app) cmdPlugin(args []string) error {
	fs := a.flags("plugin")
	install := fs.Bool("install", false, "write the plugin dir and make it Claude Code's buildermesh marketplace, installed at user scope")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return fail(exitUsage, pluginUsage)
	}
	dir := plugin.Dir(config.StateDir())
	if *install {
		return a.installPlugin(dir)
	}
	fmt.Fprintf(a.stdout, "dir: %s\n", dir)
	if _, label, ok := plugin.Written(dir); ok {
		fmt.Fprintf(a.stdout, "written by: bmesh %s\n", label)
	} else {
		fmt.Fprintln(a.stdout, "written by: none yet (bmesh plugin --install writes it)")
	}
	fmt.Fprintf(a.stdout, "claude: %s\n", claudeReads(plugin.Status(context.Background())))
	return nil
}

// claudeReads is the line saying whether Claude Code loads the plugin from
// bmesh's dir, and what to run when it doesn't.
func claudeReads(s plugin.State, err error) string {
	dir := plugin.Dir(config.StateDir())
	switch {
	case errors.Is(err, plugin.ErrNoClaude):
		return "not on PATH"
	case err != nil:
		return err.Error()
	case s.Reads(dir):
		return "reads it (" + plugin.ID + ", user scope)"
	case s.Marketplace == nil:
		return "no " + plugin.Name + " marketplace; bmesh plugin --install adds it"
	case !s.Marketplace.Is(dir):
		return fmt.Sprintf("its %s marketplace reads %s; bmesh plugin --install moves it here", plugin.Name, s.Marketplace.From())
	case s.Install == nil:
		return plugin.ID + " is not installed at user scope; bmesh plugin --install installs it"
	}
	return plugin.ID + " is disabled; bmesh plugin --install enables it"
}

// writePlugin writes the plugin this binary carries to dir unless dir
// already has it, and says so when it wrote.
func (a *app) writePlugin(dir string) error {
	label := buildVersion().String()
	wrote, err := plugin.Ensure(dir, label)
	if err != nil {
		return err
	}
	if wrote {
		fmt.Fprintf(a.stdout, "plugin: wrote %s (bmesh %s)\n", dir, label)
	}
	return nil
}

// installPlugin writes the plugin dir and points Claude Code at it,
// printing each change; a second run changes and prints nothing.
func (a *app) installPlugin(dir string) error {
	if err := a.writePlugin(dir); err != nil {
		return err
	}
	lines, err := plugin.Install(context.Background(), dir)
	for _, l := range lines {
		fmt.Fprintln(a.stdout, "plugin: "+l)
	}
	if errors.Is(err, plugin.ErrNoClaude) {
		return fail(exitRefused, "claude is not on PATH: install Claude Code, then run bmesh plugin --install")
	}
	if err != nil {
		return fail(exitError, "%v", err)
	}
	return nil
}
