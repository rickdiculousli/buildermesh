package spec

import "strings"

// Section is the body of text's `## <name>` section: the lines after the
// heading up to the next `## ` heading, trimmed; ok is false when text has
// no such heading.
func Section(text, name string) (body string, ok bool) {
	var b strings.Builder
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, "## ") {
			if in {
				break
			}
			if strings.TrimSpace(l) == "## "+name {
				in, ok = true, true
			}
			continue
		}
		if in {
			b.WriteString(l + "\n")
		}
	}
	return strings.TrimSpace(b.String()), ok
}

// UpgradingSteps is the spec's `## Upgrading` section when it names steps:
// "" when the section is absent, empty or its first word is `nothing`
// (any case, punctuation aside).
func (s *Spec) UpgradingSteps() string {
	body, _ := Section(s.Body, "Upgrading")
	if f := strings.Fields(body); len(f) == 0 || strings.EqualFold(strings.Trim(f[0], ".,:;!`*_\"'"), "nothing") {
		return ""
	}
	return body
}
