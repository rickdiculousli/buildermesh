package usage

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// runs makes n done leads of repo and size, each reading read.
func runs(repo, size string, n int, read int64) []Run {
	var out []Run
	for i := range n {
		out = append(out, Run{Agent: fmt.Sprintf("%s-%s-%d", repo, size, i), Role: "lead", Repo: repo, Size: size, Read: read, Wall: 3600})
	}
	return out
}

func rules(fs []Flag) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Rule+" "+f.Level)
	}
	return out
}

func TestEachRuleAtUnderAndOverItsThreshold(t *testing.T) {
	b := NewBaselines(runs("app", "M", 5, 10_000_000))
	lead := func(s Subject) Subject { s.Role, s.Repo, s.Size = "lead", "app", "M"; return s }
	refusal := "refused: rebase onto origin/main conflicts in a.go"
	for _, tc := range []struct {
		name string
		s    Subject
		want []string
	}{
		{"size at 2.5x", lead(Subject{Read: 25_000_000}), []string{}},
		{"size just over 2.5x", lead(Subject{Read: 25_000_001}), []string{"size overworking"}},
		{"size at 5x", lead(Subject{Read: 50_000_000}), []string{"size overworking"}},
		{"size just over 5x", lead(Subject{Read: 50_000_001}), []string{"size runaway"}},
		{"refused twice", lead(Subject{Refused: []string{refusal, refusal}}), []string{}},
		{"refused three times", lead(Subject{Refused: []string{refusal, refusal, refusal}}), []string{"refused overworking"}},
		{"refused executor", Subject{Role: "executor", Refused: []string{refusal, refusal, refusal}}, []string{}},
		{"context at compact_soon", Subject{Role: Coordinator, Ctx: 400_000}, []string{}},
		{"context just over", Subject{Role: Coordinator, Ctx: 400_001}, []string{"context runaway"}},
		{"burn at 50M", Subject{Role: "executor", Hour: 50_000_000}, []string{}},
		{"burn just over 50M", Subject{Role: "executor", Hour: 50_000_001}, []string{"burn overworking"}},
		{"burn at 100M", Subject{Role: "executor", Hour: 100_000_000}, []string{"burn overworking"}},
		{"burn just over 100M", Subject{Role: "executor", Hour: 100_000_001}, []string{"burn runaway"}},
		{"worst first", lead(Subject{Read: 30_000_000, Ctx: 500_000}), []string{"context runaway", "size overworking"}},
	} {
		if got := rules(Flags(tc.s, b, 400_000)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: flags %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSizeRuleNeedsFiveRuns(t *testing.T) {
	s := Subject{Role: "lead", Repo: "app", Size: "M", Read: 100_000_000}
	// Two in the repo and two elsewhere: neither has 5, so the rule is silent.
	b := NewBaselines(append(runs("app", "M", 2, 10_000_000), runs("web", "M", 2, 10_000_000)...))
	if fs := Flags(s, b, 0); len(fs) != 0 {
		t.Fatalf("flags %v with 4 runs in the fleet, want none", fs)
	}
	// One more elsewhere: the fleet has 5 and the repo takes its median.
	b = NewBaselines(append(runs("app", "M", 2, 10_000_000), runs("web", "M", 3, 10_000_000)...))
	fs := Flags(s, b, 0)
	if want := "read 100M, 10.0× the fleet M median (10.0M)"; len(fs) != 1 || fs[0].Words != want {
		t.Fatalf("flags %v, want %q", fs, want)
	}
	// A repo with its own 5 is judged by its own median.
	b = NewBaselines(append(runs("app", "M", 5, 30_000_000), runs("web", "M", 5, 1_000_000)...))
	if fs := Flags(s, b, 0); len(fs) != 1 || fs[0].Words != "read 100M, 3.3× the app M median (30.0M)" || fs[0].Level != Overworking {
		t.Fatalf("flags %v, want the app median", fs)
	}
	// A lead with no size, or a size no run has, is never size-flagged.
	if fs := Flags(Subject{Role: "lead", Repo: "app", Read: 1 << 40}, b, 0); len(fs) != 0 {
		t.Fatalf("flags %v for a lead with no size", fs)
	}
}

func TestSizeRuleIgnoresSmallReads(t *testing.T) {
	b := NewBaselines(runs("app", "XS", 5, 400_000))
	s := Subject{Role: "lead", Repo: "app", Size: "XS", Read: 5_000_000}
	if fs := Flags(s, b, 0); len(fs) != 0 {
		t.Fatalf("flags %v at the floor, want none", fs)
	}
	s.Read++
	if fs := Flags(s, b, 0); len(rules(fs)) != 1 || fs[0].Level != Runaway {
		t.Fatalf("flags %v just over the floor, want size runaway", fs)
	}
}

func TestFixersAreJudgedByFixers(t *testing.T) {
	var rs []Run
	for i := range 5 {
		rs = append(rs, Run{Agent: fmt.Sprintf("app-fix-%d", i), Role: "fixer", Repo: "app", Read: 2_000_000})
	}
	fs := Flags(Subject{Role: "fixer", Repo: "app", Read: 11_000_000}, NewBaselines(rs), 0)
	if want := "read 11.0M, 5.5× the app fixer median (2.0M)"; len(fs) != 1 || fs[0].Words != want || fs[0].Level != Runaway {
		t.Fatalf("flags %v, want %q runaway", fs, want)
	}
}

func TestFlagWords(t *testing.T) {
	b := NewBaselines(runs("buildermesh", "M", 5, 10_200_000))
	s := Subject{Role: "lead", Repo: "buildermesh", Size: "M", Read: 42_200_000, Ctx: 537_000, Hour: 154_000_000, Refused: []string{
		"refused: rebase onto origin/main conflicts in scripts/skilltest/cases.go",
		"refused: review failed at t:8a638215 (job x)",
		"refused: rebase onto origin/main conflicts in scripts/skilltest/arm.go",
		"refused: review failed at t:c9cb9c0b (job y)",
		"refused: rebase onto origin/main conflicts in scripts/skilltest/fake/bmesh",
	}}
	got := []string{}
	for _, f := range Flags(s, b, 400_000) {
		got = append(got, f.Words)
		if RuleOf(f.Words) != f.Rule {
			t.Errorf("RuleOf(%q) = %q, want %q", f.Words, RuleOf(f.Words), f.Rule)
		}
	}
	want := []string{
		"context 537k, past compact_soon (400k)",
		"read 154M in the last hour",
		"read 42.2M, 4.1× the buildermesh M median (10.2M)",
		"land refused 5× (3 rebase conflicts, 2 reviews failed)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("words\n%q\nwant\n%q", got, want)
	}
}

func TestHourReadKeepsTheTrailingHour(t *testing.T) {
	now := day.Add(12 * time.Hour)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339Nano) }
	calls := []Call{{At: at(61 * time.Minute), CR: 1}, {At: at(60 * time.Minute), CR: 10}, {At: at(time.Minute), In: 100, CW: 1000}}
	if got := HourRead(calls, now); got != 1110 {
		t.Fatalf("HourRead = %d, want 1110", got)
	}
}
