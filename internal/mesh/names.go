// Package mesh holds the NATS names, paths and clients of the bmesh mesh.
package mesh

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	StreamMesh    = "bm-mesh"
	BucketNodes   = "bm-nodes"
	BucketState   = "bm-state"
	BucketRunner  = "bm-runner"
	BucketResults = "bm-results"
	BucketLeases  = "bm-leases"
)

var pathRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(\.[a-z0-9][a-z0-9-]*)*$`)

var reserved = map[string]bool{"inbox": true, "routine": true, "daemon": true}

// Path names a node: dot-separated tokens, the first of which is its lead.
type Path []string

// ParsePath validates s against the path grammar and the reserved first tokens.
func ParsePath(s string) (Path, error) {
	if !pathRE.MatchString(s) {
		return nil, fmt.Errorf("invalid path %q: want lowercase tokens [a-z0-9-] joined by dots", s)
	}
	if strings.Contains(s, "--") {
		return nil, fmt.Errorf("invalid path %q: %q is not allowed (inbox consumer names map \".\" to \"--\")", s, "--")
	}
	p := Path(strings.Split(s, "."))
	if reserved[p[0]] {
		return nil, fmt.Errorf("invalid path %q: %q is a reserved first token", s, p[0])
	}
	return p, nil
}

func (p Path) String() string { return strings.Join(p, ".") }

// Lead returns the first token.
func (p Path) Lead() string {
	if len(p) == 0 {
		return ""
	}
	return p[0]
}

// Parent returns the path without its last token, false for a single token.
func (p Path) Parent() (Path, bool) {
	if len(p) < 2 {
		return nil, false
	}
	return p[:len(p)-1], true
}

func EventSubject(p Path, event string) string { return "bm." + p.String() + "." + event }

func InboxSubject(p Path) string { return "bm.inbox." + p.String() }

func DaemonSubject(host, event string) string { return "bm.daemon." + host + "." + event }

// TreeFilter matches every event under a lead.
func TreeFilter(lead string) string { return "bm." + lead + ".>" }

func InboxConsumer(p Path) string { return "inbox-" + strings.ReplaceAll(p.String(), ".", "--") }
