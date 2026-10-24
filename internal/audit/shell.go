package audit

import (
	"path"
	"slices"
	"strings"
)

// commands splits a shell command line into its simple commands' words:
// quotes make one word (and are dropped), `;`, `&&`, `||`, `|`, `&` and a
// newline end a command, a `#` comment and a heredoc's body are skipped. A
// word that runs a command line of its own (`bash -c '...'`, ssh's last
// argument) is split in turn.
// shortcut: no $(...), backticks or escapes inside double quotes beyond \"; enough for the commands sessions run, upgrade when a real transcript misreads.
func commands(s string) [][]string {
	var out [][]string
	var words []string
	var cur strings.Builder
	inWord := false
	drop := false // the next word is a redirect's target
	var heredocs []string
	end := func() {
		if inWord && !drop {
			words = append(words, cur.String())
		}
		if inWord {
			cur.Reset()
			inWord, drop = false, false
		}
	}
	flush := func() {
		end()
		drop = false
		if len(words) > 0 {
			out = append(out, words)
			out = append(out, nested(words)...)
		}
		words = nil
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			cur.WriteString(s[i+1 : i+1+j])
			inWord, i = true, i+1+j
		case c == '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
			}
			cur.WriteString(strings.ReplaceAll(s[i+1:min(j, len(s))], `\"`, `"`))
			inWord, i = true, j
		case c == '\\' && i+1 < len(s):
			if s[i+1] != '\n' {
				cur.WriteByte(s[i+1])
				inWord = true
			}
			i++
		case c == '#' && !inWord:
			for i < len(s) && s[i] != '\n' {
				i++
			}
			i--
		case c == '<' && strings.HasPrefix(s[i:], "<<") && !strings.HasPrefix(s[i:], "<<<"):
			end()
			j := i + 2
			if j < len(s) && s[j] == '-' {
				j++
			}
			for j < len(s) && s[j] == ' ' {
				j++
			}
			k := j
			for k < len(s) && !strings.ContainsRune(" \t\n;&|)", rune(s[k])) {
				k++
			}
			if d := strings.Trim(s[j:k], `'"`); d != "" {
				heredocs = append(heredocs, d)
			}
			i = k - 1
		case c == '>' || c == '<' || c == '&' && i+1 < len(s) && s[i+1] == '>':
			// A redirect: an fd before it (2>) and its target are no words.
			if inWord && strings.Trim(cur.String(), "0123456789") == "" {
				cur.Reset()
				inWord = false
			}
			end()
			for i+1 < len(s) && strings.ContainsRune(">&|", rune(s[i+1])) {
				i++
			}
			drop = true
		case c == '\n':
			flush()
			for _, d := range heredocs {
				// The body runs to the line that is the delimiter alone.
				for i+1 < len(s) {
					nl := strings.IndexByte(s[i+1:], '\n')
					line := s[i+1:]
					if nl >= 0 {
						line = s[i+1 : i+1+nl]
					}
					if nl < 0 {
						i = len(s)
					} else {
						i += 1 + nl
					}
					if strings.TrimSpace(line) == d {
						break
					}
				}
			}
			heredocs = nil
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')':
			flush()
		case c == ' ' || c == '\t':
			end()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return out
}

// nested are the commands a word of words runs as a command line of its
// own: `bash -c`/`-lc` and `sh -c`'s argument, ssh's last word.
func nested(words []string) [][]string {
	words = unwrap(words)
	if len(words) < 2 {
		return nil
	}
	switch path.Base(words[0]) {
	case "bash", "sh", "zsh":
		for i, w := range words[1 : len(words)-1] {
			if strings.HasPrefix(w, "-") && strings.HasSuffix(w, "c") {
				return commands(words[i+2])
			}
		}
	case "ssh":
		return commands(words[len(words)-1])
	}
	return nil
}

// unwrap drops what runs a command without being it: leading VAR=value
// assignments, env, nohup, time, and timeout with its duration.
func unwrap(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case strings.Contains(w, "=") && !strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "="):
			words = words[1:]
		case w == "env" || w == "nohup" || w == "time" || w == "exec":
			words = words[1:]
		case w == "timeout" && len(words) > 1:
			words = words[2:]
		default:
			return words
		}
	}
	return words
}

// call is a bmesh command line: its command and the words after it, the
// global flags before it left out.
type call struct {
	cmd  string
	args []string
}

// bmeshCalls are the bmesh commands a shell command line runs.
func bmeshCalls(s string) []call {
	var out []call
	for _, words := range commands(s) {
		if c, ok := bmeshCall(unwrap(words)); ok {
			out = append(out, c)
		}
	}
	return out
}

// bmeshCall is a simple command's words as a bmesh call, when it is one.
func bmeshCall(words []string) (call, bool) {
	if len(words) == 0 || path.Base(words[0]) != "bmesh" {
		return call{}, false
	}
	rest := words[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		name, _, inline := strings.Cut(strings.TrimLeft(rest[0], "-"), "=")
		rest = rest[1:]
		if (name == "node" || name == "config") && !inline && len(rest) > 0 {
			rest = rest[1:]
		}
	}
	if len(rest) == 0 {
		return call{}, false
	}
	return call{rest[0], rest[1:]}, true
}

// sub is the call's subcommand, `start` of `lead start`: the word right
// after a command that has subcommands, "" for any other.
func (c call) sub() string {
	if _, ok := reads[c.cmd]; ok && reads[c.cmd] != nil || slices.Contains(subcommanded, c.cmd) {
		if len(c.args) > 0 && !strings.HasPrefix(c.args[0], "-") {
			return c.args[0]
		}
	}
	return ""
}

// subcommanded are the commands with subcommands that change something
// beside those reads lists.
var subcommanded = []string{"lead", "node", "result", "t3", "lock-lease"}

func (c call) has(flag string) bool {
	return slices.ContainsFunc(c.args, func(a string) bool {
		n, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		return strings.HasPrefix(a, "-") && n == flag
	})
}

// String is the command and its subcommand: `lead start`.
func (c call) String() string { return strings.TrimSpace(c.cmd + " " + c.sub()) }

// reports are the events a node reports with: they say, never change, what
// the work is.
var reports = []string{"started", "progress", "ask", "blocked", "held"}

// reads are the commands that only read, by command, with the
// subcommands that read ("" for none given); a command not here starts or
// changes something.
var reads = map[string][]string{
	"mesh-status": nil, "inbox": nil, "watch": nil, "context": nil, "audit": nil,
	"whoami": nil, "thread-status": nil, "version": nil, "help": nil, "plugin": nil,
	"repos":    {"", "show"},
	"hosts":    {"", "replies", "held", "ping"},
	"spec":     {"list", "show", "steps", "template", "next-id"},
	"routine":  {"list", "wait"},
	"executor": {"list"},
	"feedback": {"list"},
	"config":   {"", "get", "show", "check"},
}

// acts reports whether c starts or changes something.
func (c call) acts() bool {
	if slices.Contains(reports, c.cmd) || c.has("help") || c.has("h") || c.has("dry-run") || c.has("check") && c.cmd != "spec" {
		return false
	}
	subs, ok := reads[c.cmd]
	if !ok {
		return true
	}
	if subs == nil {
		return false
	}
	if c.cmd == "spec" && c.sub() == "steps" && c.has("check") {
		return true
	}
	return !slices.Contains(subs, c.sub())
}

// pushesMain reports whether a shell command line pushes to main: a git
// push naming `main` as a refspec's destination (`main`, `HEAD:main`,
// `refs/heads/main`).
func pushesMain(s string) bool {
	for _, words := range commands(s) {
		words = unwrap(words)
		if len(words) == 0 || path.Base(words[0]) != "git" {
			continue
		}
		i := 1
		for i < len(words) && strings.HasPrefix(words[i], "-") {
			if words[i] == "-C" || words[i] == "-c" {
				i++
			}
			i++
		}
		if i >= len(words) || words[i] != "push" {
			continue
		}
		var pos []string
		for _, w := range words[i+1:] {
			if !strings.HasPrefix(w, "-") {
				pos = append(pos, w)
			}
		}
		for _, ref := range pos[min(1, len(pos)):] {
			_, dst, found := strings.Cut(strings.TrimPrefix(ref, "+"), ":")
			if !found {
				dst = strings.TrimPrefix(ref, "+")
			}
			if dst == "main" || dst == "refs/heads/main" {
				return true
			}
		}
	}
	return false
}

// fileRead is the file a shell command line only reads, and how much:
// one cat, sed -n 'a,bp', head or tail of one file, after any cd.
func fileRead(s string) (file, rng string, ok bool) {
	var cmds [][]string
	for _, words := range commands(s) {
		if words = unwrap(words); len(words) > 0 && words[0] != "cd" {
			cmds = append(cmds, words)
		}
	}
	if len(cmds) != 1 {
		return "", "", false
	}
	words := cmds[0]
	var pos, flags []string
	for _, w := range words[1:] {
		if strings.HasPrefix(w, "-") {
			flags = append(flags, w)
		} else {
			pos = append(pos, w)
		}
	}
	switch path.Base(words[0]) {
	case "cat":
		if len(pos) == 1 && len(flags) == 0 {
			return pos[0], "whole", true
		}
	case "sed":
		if len(pos) == 2 && slices.Equal(flags, []string{"-n"}) {
			if a, b, found := strings.Cut(strings.TrimSuffix(pos[0], "p"), ","); found && digits(a) && digits(b) {
				return pos[1], "lines " + a + "-" + b, true
			}
		}
	case "head", "tail":
		if n := len(pos); n == 1 || n == 2 && len(flags) == 1 && flags[0] == "-n" && digits(pos[0]) {
			return pos[n-1], words[0] + " " + strings.Join(append(flags, pos[:n-1]...), " "), true
		}
	}
	return "", "", false
}

func digits(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }

// shellActs is the first command of a shell command line that changes
// something, in the order it runs: a git commit or push, or a bmesh
// command that acts; "" when it only reads.
func shellActs(s string) string {
	for _, words := range commands(s) {
		words = unwrap(words)
		if len(words) == 0 {
			continue
		}
		switch path.Base(words[0]) {
		case "bmesh":
			if c, ok := bmeshCall(words); ok && c.acts() {
				return "bmesh " + c.String()
			}
		case "git":
			for i := 1; i < len(words); i++ {
				w := words[i]
				if w == "-C" || w == "-c" {
					i++
					continue
				}
				if strings.HasPrefix(w, "-") {
					continue
				}
				if w == "commit" || w == "push" {
					return "git " + w
				}
				break
			}
		}
	}
	return ""
}
