package upgrade

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Cond is one comparison of a step's `when`: `[section] key == "value"`
// or `!=`.
type Cond struct {
	Section, Key string
	Equal        bool
	Value        string
}

// condRE is one comparison; the value is quoted, or a bare word.
var condRE = regexp.MustCompile(`^\[\s*([A-Za-z0-9_]+)\s*\]\s*([A-Za-z0-9_]+)\s*(==|!=)\s*("(?:[^"\\]|\\.)*"|[A-Za-z0-9_.:/-]+)$`)

// When is a step's condition: comparisons that must all hold. An empty one
// always holds.
type When []Cond

// ParseWhen reads `<cond> [&& <cond>]...`.
func ParseWhen(s string) (When, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var w When
	for _, part := range strings.Split(s, "&&") {
		m := condRE.FindStringSubmatch(strings.TrimSpace(part))
		if m == nil {
			return nil, fmt.Errorf(`when %q: want [section] key == "value" (or !=), joined by &&`, s)
		}
		v := m[4]
		if strings.HasPrefix(v, `"`) {
			u, err := strconv.Unquote(v)
			if err != nil {
				return nil, fmt.Errorf("when %q: %v", s, err)
			}
			v = u
		}
		w = append(w, Cond{Section: m[1], Key: m[2], Equal: m[3] == "==", Value: v})
	}
	return w, nil
}

// Holds reports whether every comparison holds, get reading the config's
// values (config.Values); a key it doesn't have reads as "".
func (w When) Holds(get func(section, key string) (string, bool)) bool {
	for _, c := range w {
		v, _ := get(c.Section, c.Key)
		if (v == c.Value) != c.Equal {
			return false
		}
	}
	return true
}
