package spec

import (
	"slices"
	"testing"
)

func commandSpec(t *testing.T, id, status, proposal string) *Spec {
	t.Helper()
	s, err := Parse("---\nid: "+id+"\ntitle: T\nstatus: "+status+"\n---\n\n## Opportunity\n**`bmesh early`** is not the Proposal.\n\n## Proposal\n"+proposal+"\n\n## Risks\n**`bmesh late`**\n", id+"-t.md")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCommandsAreTheBoldSynopses(t *testing.T) {
	s := commandSpec(t, "048", "accepted", "In:\n"+
		"- **`bmesh hold <path>|--family F [--gate land|now]`** writes the key.\n"+
		"- **`bmesh release <repo> [--minor|--patch]`.** It tags. **`bmesh feedback list <family>`** prints, and so does **`bmesh hold <path>`**.\n"+
		"**`bmesh sync [--repo R]`:**\n"+
		"- **`bmesh build --hold land|now`** adds a flag.\n"+
		"- **`bmesh build` repeats the warning**, a command talked about.\n"+
		"- `bmesh map` in plain code, and **bold `bmesh map`** with the span inside.\n"+
		"- **`bmesh --node P`** names no command.\n"+
		"- A definition reads `**`bmesh quoted <x>`**`, quoted, as does ``**`bmesh quoted`**``.")
	if got, want := s.Commands(), []string{"hold", "release", "feedback list", "sync"}; !slices.Equal(got, want) {
		t.Fatalf("commands %q, want %q", got, want)
	}
}

func TestCommandClashes(t *testing.T) {
	s := commandSpec(t, "048", "accepted", "- **`bmesh release <path>`** lifts a hold.\n- **`bmesh land`** again.\n- **`bmesh map`**")
	others := []*Spec{
		s,
		commandSpec(t, "046", "accepted", "- **`bmesh release <repo>`** tags."),
		commandSpec(t, "047", "building", "- **`bmesh release`**"),
		commandSpec(t, "045", "done", "- **`bmesh release`**"),
		commandSpec(t, "044", "proposed", "- **`bmesh map`**"),
		commandSpec(t, "043", "accepted", "Uses `bmesh map` and **`bmesh map` is fast**."),
	}
	got := CommandClashes(s, others, []string{"land", "spec verified"})
	want := []string{
		"048 defines bmesh release, which 046 (accepted) also defines",
		"048 defines bmesh release, which 047 (building) also defines",
		"048 defines bmesh land, which main already has",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("clashes %q, want %q", got, want)
	}
}

func TestUsageCommands(t *testing.T) {
	usage := "usage: bmesh <command>\n\n--config is accepted.\n\ncommands:\n" +
		"  init [--service] [--force]\n" +
		"                                           set this box up\n" +
		"  whoami                                   print this node's path\n" +
		"  started|progress|blocked|ask [--text T]  report an event\n" +
		"  hosts invite <name> [--secure]\n" +
		"  t3 login --status                        print the sign-in\n" +
		"  spec verified [--spec NNN]\n" +
		"  update --plan [--json]                   the walk\n"
	want := []string{"init", "whoami", "started", "progress", "blocked", "ask", "hosts invite", "t3 login", "spec verified", "update"}
	if got := UsageCommands(usage); !slices.Equal(got, want) {
		t.Fatalf("%q, want %q", got, want)
	}
}
