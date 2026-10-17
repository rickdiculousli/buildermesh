package spec

import "testing"

func TestUpgradingSteps(t *testing.T) {
	for body, want := range map[string]string{
		"## Proposal\nx\n": "",
		"## Upgrading\n\n## Needs from the operator\nask\n":                 "",
		"## Upgrading\nnothing\n":                                           "",
		"## Upgrading\n`Nothing.`\n":                                        "",
		"## Upgrading\nNothing for an install at v0.1.0 beyond.\n":          "",
		"## Upgrading\nRun `bmesh hub`.\n\n## Needs from the operator\nx\n": "Run `bmesh hub`.",
		"## Upgrading\nNothingness is not a word.\n":                        "Nothingness is not a word.",
	} {
		s := &Spec{Body: body}
		if got := s.UpgradingSteps(); got != want {
			t.Errorf("%q: got %q, want %q", body, got, want)
		}
	}
	if _, ok := Section("## Upgradings\nx\n", "Upgrading"); ok {
		t.Fatal("a longer heading matched")
	}
}
