// Package cligraph turns a command line's --help pages into a graph: one node
// per command, flag and positional argument, with the edges between them, and
// an edge from each command to the function that implements it when the
// repository's code graph has one.
//
// The help pages were already ingested as a document (ingest.CLIHelp), and a
// document is the wrong shape for the questions an operator asks about a
// command line. Asked which flag sets how long `drbd-reactorctl evict` waits
// for the peer, retrieval returned the second half of the evict page — the
// --unmask flag — and not the chunk holding --delay and its default of 20:
// the page is split wherever the chunker runs out of room, and a flag's line
// is a few words among two hundred. A flag that is its own node, carrying its
// command, value, default and description, is retrieved for what it is.
package cligraph

import (
	"bufio"
	"regexp"
	"strings"
)

// Page is one command's --help output, as the walk collected it.
type Page struct {
	Path []string // subcommand names below the binary; empty for the binary itself
	Help string
}

// Command is what one help page says about one command.
type Command struct {
	Path        []string
	About       string
	Usage       string
	Flags       []Flag
	Args        []Arg
	Subcommands []string
}

// Flag is one option. Value is the placeholder an option takes ("<delay>",
// "int"); a switch has none.
type Flag struct {
	Short    string
	Long     string
	Value    string
	Default  string
	Possible []string
	Desc     string
	Global   bool
}

// Name is the flag as an operator types it: the long form when there is one.
func (f Flag) Name() string {
	if f.Long != "" {
		return f.Long
	}
	return f.Short
}

// Arg is one positional argument.
type Arg struct {
	Name     string
	Variadic bool
	Desc     string
}

type section int

const (
	secNone section = iota
	secUsage
	secFlags
	secGlobalFlags
	secArgs
	secCommands
	secOther
)

// headerRe is a section heading: unindented, a name, a colon, and possibly the
// section's first line on the same line ("Usage: tool [OPTIONS]").
var headerRe = regexp.MustCompile(`^([A-Za-z][A-Za-z ]{0,30}):(?:\s+(.*))?$`)

func classify(name string) section {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(n, "usage"):
		return secUsage
	case strings.Contains(n, "global") && (strings.Contains(n, "flag") || strings.Contains(n, "option")):
		return secGlobalFlags
	case strings.Contains(n, "command"):
		return secCommands
	case strings.Contains(n, "flag") || strings.Contains(n, "option"):
		return secFlags
	case strings.Contains(n, "arg") || strings.Contains(n, "positional"):
		return secArgs
	}
	return secOther
}

// entryLine is one indented line of a section with the lines that continue it.
type entryLine struct {
	head string
	more []string
}

// Parse reads one help page. It understands the layouts of cobra, clap (2 and
// 4) and argparse, which between them cover most command lines; anything it
// does not recognise is left out rather than guessed at.
func Parse(page Page) Command {
	cmd := Command{Path: append([]string(nil), page.Path...)}
	var about []string
	sec := secNone
	sections := map[section][]entryLine{}
	sc := bufio.NewScanner(strings.NewReader(page.Help))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		if line != "" && !startsIndented(line) {
			if m := headerRe.FindStringSubmatch(line); m != nil {
				sec = classify(m[1])
				if rest := strings.TrimSpace(m[2]); rest != "" {
					sections[sec] = append(sections[sec], entryLine{head: "  " + rest})
				}
				continue
			}
			if sec == secNone {
				about = append(about, line)
				continue
			}
		}
		if sec == secNone || strings.TrimSpace(line) == "" {
			continue
		}
		entries := sections[sec]
		if len(entries) > 0 && isContinuation(line, entries[len(entries)-1].head, sec) {
			entries[len(entries)-1].more = append(entries[len(entries)-1].more, strings.TrimSpace(line))
		} else {
			entries = append(entries, entryLine{head: line})
		}
		sections[sec] = entries
	}

	cmd.About = aboutOf(about, page.Path)
	for _, e := range sections[secUsage] {
		cmd.Usage = strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(e.head)}, e.more...), " "))
		break
	}
	for _, s := range []section{secFlags, secGlobalFlags} {
		for _, e := range sections[s] {
			if f, ok := parseFlag(e); ok {
				if f.Long == "--help" || f.Long == "--version" {
					continue // every command has them; they document the help system
				}
				f.Global = s == secGlobalFlags
				cmd.Flags = append(cmd.Flags, f)
			}
		}
	}
	for _, e := range sections[secArgs] {
		if a, ok := parseArg(e); ok {
			cmd.Args = append(cmd.Args, a)
		}
	}
	for _, e := range sections[secCommands] {
		fields := strings.Fields(e.head)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ",")
		if name == "help" || name == "completion" || strings.HasPrefix(name, "-") {
			continue
		}
		cmd.Subcommands = append(cmd.Subcommands, name)
	}
	return cmd
}

func startsIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// isContinuation reports whether line continues the entry headed by head: it
// is indented further and, in a flag section, does not start a new flag.
func isContinuation(line, head string, sec section) bool {
	if indentOf(line) <= indentOf(head) {
		return false
	}
	t := strings.TrimSpace(line)
	if sec == secFlags || sec == secGlobalFlags {
		return !strings.HasPrefix(t, "-")
	}
	if sec == secArgs {
		return !strings.HasPrefix(t, "<")
	}
	return true
}

// aboutOf is the description above the first section, without the lines that
// only name the program, its version or its authors.
func aboutOf(lines []string, path []string) string {
	var keep []string
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if i == 0 && looksLikeTitle(t, path) {
			continue
		}
		if strings.Contains(t, "@") && strings.Contains(t, "<") {
			continue // "Name <name@example.com>"
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, " ")
}

// looksLikeTitle is clap's first line: the program name and version, or the
// program and subcommand joined with a dash.
func looksLikeTitle(line string, path []string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || len(fields) > 2 {
		return false
	}
	if len(path) > 0 && strings.HasSuffix(fields[0], "-"+strings.Join(path, "-")) {
		return true
	}
	return len(fields) == 2 && versionRe.MatchString(fields[1])
}

var versionRe = regexp.MustCompile(`^v?\d+(\.\d+)+`)

// flagRe matches the start of a flag entry: an optional short form, an optional
// long form, an optional value, then — after two spaces — the description.
var flagRe = regexp.MustCompile(`^\s*(?:(-[A-Za-z0-9?@])(?:,\s*|\s+|$))?(--[A-Za-z0-9][\w.-]*)?(?:[ =]((?:<[^>]+>|\[[^\]]+\]|[A-Za-z_][\w|-]*)(?:\.\.\.)?))?(?:\s{2,}(.*))?$`)

var (
	defaultRe  = regexp.MustCompile(`\[default:\s*([^\]]*)\]|\(default:?\s*([^)]*)\)`)
	possibleRe = regexp.MustCompile(`\[possible values:\s*([^\]]*)\]`)
)

func parseFlag(e entryLine) (Flag, bool) {
	m := flagRe.FindStringSubmatch(e.head)
	if m == nil || (m[1] == "" && m[2] == "") {
		return Flag{}, false
	}
	f := Flag{Short: m[1], Long: m[2], Value: m[3]}
	desc := strings.TrimSpace(strings.Join(append([]string{m[4]}, e.more...), " "))
	if d := defaultRe.FindStringSubmatch(desc); d != nil {
		f.Default = strings.Trim(strings.TrimSpace(d[1]+d[2]), `"`)
	}
	if p := possibleRe.FindStringSubmatch(desc); p != nil {
		for _, v := range strings.Split(p[1], ",") {
			if v = strings.TrimSpace(v); v != "" {
				f.Possible = append(f.Possible, v)
			}
		}
	}
	f.Desc = strings.Join(strings.Fields(desc), " ")
	return f, true
}

var argRe = regexp.MustCompile(`^\s*(<[^>]+>|[A-Za-z][\w-]*)(\.\.\.)?(?:\s{2,}(.*))?$`)

func parseArg(e entryLine) (Arg, bool) {
	m := argRe.FindStringSubmatch(e.head)
	if m == nil {
		return Arg{}, false
	}
	desc := strings.TrimSpace(strings.Join(append([]string{m[3]}, e.more...), " "))
	return Arg{Name: m[1], Variadic: m[2] != "", Desc: strings.Join(strings.Fields(desc), " ")}, true
}
