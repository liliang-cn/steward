package cligraph

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/liliang-cn/steward/internal/knowledge"
)

// The types this package writes. They are steward's own reading of a command
// line, not a vocabulary any domain.toml declares, so they are exported for the
// store to be told what to expect, as graphimport does for layers and tours.
const (
	CommandType  = "cli_command"
	FlagType     = "cli_flag"
	ArgumentType = "cli_argument"

	HasSubcommandType = "has_subcommand"
	HasFlagType       = "has_flag"
	HasArgumentType   = "has_argument"
	// ImplementedByType runs from a command to the function in the code graph
	// that handles it.
	ImplementedByType = "implemented_by"
)

var (
	EntityTypes   = []string{CommandType, FlagType, ArgumentType}
	RelationTypes = []string{HasSubcommandType, HasFlagType, HasArgumentType, ImplementedByType}
)

// DocID is the document a binary's command graph is filed under: the same
// document as its help reference, so purging one purges both.
func DocID(bin string) string { return bin + "-help" }

// Plan is what Import would write.
type Plan struct {
	Bin       string
	DocID     string
	Embed     map[string]string
	Entities  []knowledge.GraphEntity
	Relations []knowledge.GraphRelation
	Commands  []Command
	// CodePath is where the command line's code lives, for a binary whose
	// source is not named after it (haify's is cmd/cli). Empty: files and
	// directories named for the binary.
	CodePath string
}

func commandID(bin string, p []string) string {
	if len(p) == 0 {
		return "cli:" + bin
	}
	return "cli:" + bin + ":" + strings.Join(p, ":")
}

func commandName(bin string, p []string) string {
	return strings.TrimSpace(bin + " " + strings.Join(p, " "))
}

// Build plans the graph for one binary from its parsed help pages.
func Build(bin string, cmds []Command) *Plan {
	p := &Plan{Bin: bin, DocID: DocID(bin), Embed: map[string]string{}, Commands: cmds}
	known := map[string]bool{}
	for _, c := range cmds {
		known[commandID(bin, c.Path)] = true
	}
	for _, c := range cmds {
		id := commandID(bin, c.Path)
		name := commandName(bin, c.Path)
		p.Embed[id] = commandText(name, c)
		meta := map[string]string{"cli": bin, "command": name}
		if c.Usage != "" {
			meta["usage"] = c.Usage
		}
		p.Entities = append(p.Entities, knowledge.GraphEntity{
			ID: id, Name: name, Type: CommandType, Description: commandSummary(name, c),
			Metadata: meta, ChunkIDs: []string{id},
		})

		for _, sub := range c.Subcommands {
			child := commandID(bin, append(append([]string(nil), c.Path...), sub))
			if !known[child] {
				continue // its page was not walked (failed, or the walk was capped)
			}
			p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: child, Type: HasSubcommandType,
				Metadata: map[string]string{"cli": bin}})
		}

		for _, f := range c.Flags {
			fid := id + ":" + f.Name()
			p.Embed[fid] = flagText(name, f)
			fm := map[string]string{"cli": bin, "command": name, "flag": f.Name()}
			for k, v := range map[string]string{"short": f.Short, "long": f.Long, "value": f.Value, "default": f.Default,
				"possible_values": strings.Join(f.Possible, ",")} {
				if v != "" {
					fm[k] = v
				}
			}
			if f.Global {
				fm["global"] = "true"
			}
			p.Entities = append(p.Entities, knowledge.GraphEntity{
				ID: fid, Name: name + " " + f.Name(), Type: FlagType, Description: flagText(name, f),
				Metadata: fm, ChunkIDs: []string{fid},
			})
			p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: fid, Type: HasFlagType,
				Metadata: map[string]string{"cli": bin}})
		}

		for _, a := range c.Args {
			aid := id + ":" + a.Name
			text := argText(name, a)
			p.Embed[aid] = text
			am := map[string]string{"cli": bin, "command": name, "argument": a.Name}
			if a.Variadic {
				am["variadic"] = "true"
			}
			p.Entities = append(p.Entities, knowledge.GraphEntity{
				ID: aid, Name: name + " " + a.Name, Type: ArgumentType, Description: text,
				Metadata: am, ChunkIDs: []string{aid},
			})
			p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: aid, Type: HasArgumentType,
				Metadata: map[string]string{"cli": bin}})
		}
	}
	return p
}

// commandText is what a command is retrieved by: its whole synopsis, every
// flag on its own line, so a question about one flag finds the command too.
func commandText(name string, c Command) string {
	var b strings.Builder
	b.WriteString(commandSummary(name, c))
	if c.Usage != "" {
		b.WriteString("\nUsage: " + c.Usage)
	}
	for _, f := range c.Flags {
		b.WriteString("\n  " + flagSpelling(f))
		if f.Desc != "" {
			b.WriteString(" — " + f.Desc)
		}
	}
	for _, a := range c.Args {
		b.WriteString("\n  " + argSpelling(a))
		if a.Desc != "" {
			b.WriteString(" — " + a.Desc)
		}
	}
	if len(c.Subcommands) > 0 {
		b.WriteString("\nSubcommands: " + strings.Join(c.Subcommands, ", "))
	}
	return b.String()
}

func commandSummary(name string, c Command) string {
	if c.About == "" {
		return "Command: " + name
	}
	return "Command: " + name + " — " + c.About
}

func flagSpelling(f Flag) string {
	s := f.Long
	if f.Short != "" && f.Long != "" {
		s = f.Short + ", " + f.Long
	} else if f.Long == "" {
		s = f.Short
	}
	if f.Value != "" {
		s += " " + f.Value
	}
	return s
}

func flagText(command string, f Flag) string {
	s := "Option of `" + command + "`: " + flagSpelling(f)
	if f.Desc != "" {
		s += " — " + f.Desc
	}
	if f.Default != "" && !strings.Contains(f.Desc, f.Default) {
		s += " (default: " + f.Default + ")"
	}
	if f.Global {
		s += " (global option, accepted by every subcommand)"
	}
	return s
}

func argSpelling(a Arg) string {
	if a.Variadic {
		return a.Name + "..."
	}
	return a.Name
}

func argText(command string, a Arg) string {
	s := "Positional argument of `" + command + "`: " + argSpelling(a)
	if a.Desc != "" {
		s += " — " + a.Desc
	}
	return s
}

// CodeSymbol is a function (or method) in a repository's code graph.
type CodeSymbol struct {
	ID       string // graph node id
	Name     string
	FilePath string
	Summary  string
	Calls    []string
}

// Link adds an implemented_by edge from each command to the function that
// handles it, and reports the commands it could not place.
//
// The match is by name, and deliberately narrow: the function must be named
// for the subcommand (start-until → start_until, startUntil, StartUntil, or
// runStartUntil / newStartUntilCmd for cobra's conventions) and live in a file
// named for the binary or under a directory that is. A name match anywhere in
// the repository is not a link — drbd-reactor has a `status` in the daemon as
// well as in drbd-reactorctl, and an edge to the wrong one is worse than none:
// it reads as the answer. Each edge records it was matched by name.
//
// The handler is also written into the command's own text, with the code
// graph's summary of it. The edge alone did not reach the answer: retrieval
// expands a few neighbours per hit, and a command's flags and its siblings
// under the root filled them before the handler did, so "how is restart
// --with-targets implemented" found the command and had to search the code
// again for what the command's node already knew.
func (p *Plan) Link(symbols []CodeSymbol) (unlinked []string) {
	entity := map[string]int{}
	for i, e := range p.Entities {
		entity[e.ID] = i
	}
	byKey := map[string][]CodeSymbol{}
	for _, s := range symbols {
		if !belongsToBinary(s.FilePath, p.Bin) && !underPath(s.FilePath, p.CodePath) {
			continue
		}
		byKey[normalize(s.Name)] = append(byKey[normalize(s.Name)], s)
	}
	for _, c := range p.Commands {
		var keys []string
		if len(c.Path) == 0 {
			keys = []string{"main"}
		} else {
			last := c.Path[len(c.Path)-1]
			joined := strings.Join(c.Path, "_")
			keys = []string{last, joined, "run" + last, "run" + joined, "new" + last + "cmd", "new" + joined + "cmd", last + "cmd", joined + "cmd"}
		}
		var hits []CodeSymbol
		seen := map[string]bool{}
		for _, k := range keys {
			for _, s := range byKey[normalize(k)] {
				if !seen[s.ID] {
					seen[s.ID] = true
					hits = append(hits, s)
				}
			}
		}
		name := commandName(p.Bin, c.Path)
		if len(hits) != 1 {
			unlinked = append(unlinked, name)
			continue
		}
		id := commandID(p.Bin, c.Path)
		h := hits[0]
		p.Relations = append(p.Relations, knowledge.GraphRelation{
			From: id, To: h.ID, Type: ImplementedByType,
			Metadata: map[string]string{"cli": p.Bin, "resolution": "name", "file_path": h.FilePath},
		})
		impl := "Implemented by `" + h.Name + "` in " + h.FilePath
		if h.Summary != "" {
			impl += ": " + strings.TrimSpace(h.Summary)
		}
		if !strings.HasSuffix(impl, ".") {
			impl += "."
		}
		// What the handler calls is the next thing asked ("evict calls
		// what?"), and it is two hops from the command — further than
		// retrieval expands, and behind the command's own flags when it does.
		if len(h.Calls) > 0 {
			impl += " It calls " + strings.Join(backticked(h.Calls), ", ") + "."
		}
		// Second line, under the command's one-line summary: retrieval shows
		// the start of a hit, and a long synopsis would push this out of it.
		p.Embed[id] = insertAfterFirstLine(p.Embed[id], impl)
		if i, ok := entity[id]; ok {
			p.Entities[i].Description = insertAfterFirstLine(p.Entities[i].Description, impl)
			p.Entities[i].Metadata["implemented_by"] = h.Name
			p.Entities[i].Metadata["implemented_in"] = h.FilePath
		}
	}
	sort.Strings(unlinked)
	return unlinked
}

func backticked(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "`" + n + "`"
	}
	return out
}

func insertAfterFirstLine(text, line string) string {
	first, rest, ok := strings.Cut(text, "\n")
	if !ok {
		return text + "\n" + line
	}
	return first + "\n" + line + "\n" + rest
}

// normalize folds the spellings one name takes across languages: start-until,
// start_until, startUntil and StartUntil are the same key.
func normalize(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(s))
}

func underPath(file, dir string) bool {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" || file == "" {
		return false
	}
	return strings.HasPrefix(strings.TrimPrefix(file, "./"), dir+"/")
}

func belongsToBinary(file, bin string) bool {
	if file == "" {
		return false
	}
	want := normalize(bin)
	base := strings.TrimSuffix(path.Base(file), path.Ext(file))
	if normalize(base) == want {
		return true
	}
	for _, dir := range strings.Split(path.Dir(file), "/") {
		if normalize(dir) == want {
			return true
		}
	}
	return false
}

// Stats summarizes an import.
type Stats struct {
	Commands, Flags, Args, Links int
}

// Import writes the plan. The caller purges the binary's previous document
// first, as ingest-cli does for the help reference filed under the same id.
func Import(ctx context.Context, store *knowledge.Store, p *Plan) (Stats, error) {
	var st Stats
	meta := map[string]string{"document_id": p.DocID, "source": knowledge.SourceCLIReference, "cli": p.Bin}
	if err := store.EmbedBatch(ctx, p.Embed, meta); err != nil {
		return st, fmt.Errorf("embed: %w", err)
	}
	if err := store.UpsertEntities(ctx, p.DocID, p.Entities); err != nil {
		return st, fmt.Errorf("upsert entities: %w", err)
	}
	if err := store.UpsertRelations(ctx, p.DocID, p.Relations); err != nil {
		return st, fmt.Errorf("upsert relations: %w", err)
	}
	for _, e := range p.Entities {
		switch e.Type {
		case CommandType:
			st.Commands++
		case FlagType:
			st.Flags++
		case ArgumentType:
			st.Args++
		}
	}
	for _, r := range p.Relations {
		if r.Type == ImplementedByType {
			st.Links++
		}
	}
	return st, nil
}
