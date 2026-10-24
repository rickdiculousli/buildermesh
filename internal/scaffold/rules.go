package scaffold

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// RulesBlock is AGENTS.md's fourth block, appended after mesh: the rule sets
// the repo picked on its opening marker, which is the pick's only record.
const RulesBlock = "rules"

// DefaultRules is the pick a repo with no rules block gets.
var DefaultRules = []string{"core"}

// RuleSets are the embedded sets' names, in the order a refusal lists them.
var RuleSets = []string{"core", "design", "tests", "tdd", "prose", "ponytail"}

// Customized is the opening marker's word for a block whose body differs
// from what scaffold last wrote; such a block is never rewritten.
const Customized = "customized"

//go:embed rules/*.md
var ruleFiles embed.FS

// RuleSet is the embedded text of the named set, Markdown with no heading;
// ok is false for a name bmesh doesn't ship.
func RuleSet(name string) (text string, ok bool) {
	if !slices.Contains(RuleSets, name) {
		return "", false
	}
	b, err := ruleFiles.ReadFile("rules/" + name + ".md")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// RulesBody is the block body scaffold writes for a pick: the heading, the
// line saying where the sets came from and that the rest of the file wins,
// then each set's lines under its name, in the pick's order. An empty pick
// has no body.
func RulesBody(names []string) string {
	if len(names) == 0 {
		return ""
	}
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "`" + n + "`"
	}
	sets := "rule sets"
	if len(names) == 1 {
		sets = "rule set"
	}
	var b strings.Builder
	b.WriteString("## Engineering rules\n")
	fmt.Fprintf(&b, "From bmesh's %s %s. Where the rest of this file says otherwise, the rest wins. ", sets, strings.Join(q, ", "))
	b.WriteString("Edit them freely: `bmesh scaffold` keeps an edited block and marks it customized.\n")
	for _, n := range names {
		text, _ := RuleSet(n)
		fmt.Fprintf(&b, "\n### %s\n%s\n", n, text)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// RulesHash is the first 8 hex of the sha256 of a body, its line endings
// read as \n and its surrounding whitespace trimmed, so a checkout that
// writes CRLF or an editor that adds a final newline doesn't read as an edit.
func RulesHash(body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:8]
}

// rulesRE is the rules block: the opening marker's words, then the body.
var rulesRE = regexp.MustCompile(`(?s)<!-- ` + regexp.QuoteMeta(MarkerPrefix+RulesBlock) + `\b([^>]*)-->(.*?)<!-- /` + regexp.QuoteMeta(MarkerPrefix+RulesBlock) + ` -->`)

var hashRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// rulesBlock is the whole block for a pick, its marker carrying the names
// and the body's hash; an empty pick is the two markers alone.
func rulesBlock(names []string) string {
	open, end := "<!-- "+MarkerPrefix+RulesBlock, "<!-- /"+MarkerPrefix+RulesBlock+" -->"
	if len(names) == 0 {
		return open + " -->\n" + end
	}
	body := RulesBody(names)
	return open + " " + strings.Join(names, " ") + " " + RulesHash(body) + " -->\n" + body + "\n" + end
}

// ApplyRules is text with its rules block as the marker asks: a missing
// block appended as DefaultRules, after a blank line; a marker naming sets
// (with no hash, or with the hash of the body it holds) rendered from them;
// an empty marker left with no body; a body that no longer matches its
// marker's hash kept, its marker rewritten to customized; a customized
// block left as it is. customized reports either of the last two. A name
// bmesh doesn't ship is an error. Only the first rules block is read, and
// nothing outside it changes.
func ApplyRules(text string) (out string, customized bool, err error) {
	m := rulesRE.FindStringSubmatchIndex(text)
	if m == nil {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		return text + rulesBlock(DefaultRules) + "\n", false, nil
	}
	words := strings.Fields(text[m[2]:m[3]])
	body := text[m[4]:m[5]]
	if len(words) == 1 && words[0] == Customized {
		return text, true, nil
	}
	hash := ""
	if n := len(words); n > 0 && hashRE.MatchString(words[n-1]) {
		hash, words = words[n-1], words[:n-1]
	}
	var names []string
	for _, w := range words {
		if _, ok := RuleSet(w); !ok {
			return "", false, fmt.Errorf("%s: unknown rule set %s; bmesh has %s", Agents, w, strings.Join(RuleSets, ", "))
		}
		if !slices.Contains(names, w) {
			names = append(names, w)
		}
	}
	b := rulesBlock(names)
	if hash != "" && len(names) > 0 && RulesHash(body) != hash {
		b = "<!-- " + MarkerPrefix + RulesBlock + " " + Customized + " -->" + body + "<!-- /" + MarkerPrefix + RulesBlock + " -->"
		customized = true
	}
	return text[:m[0]] + b + text[m[1]:], customized, nil
}

// RulesNote is what scaffold prints for a customized rules block.
const RulesNote = Agents + ": rules are customized; kept as written"
