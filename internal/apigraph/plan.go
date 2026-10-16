package apigraph

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/liliang-cn/steward/internal/knowledge"
)

// The types this package writes, exported for the store to be told what to
// expect from an import.
const (
	OperationType = "api_operation"
	SchemaType    = "api_schema"

	AcceptsType    = "accepts"
	ReturnsType    = "returns"
	ReferencesType = "references"
	// ImplementedByType runs from an operation to the server function that
	// handles it — the same relation a command has to its handler.
	ImplementedByType = "implemented_by"
	// CallsAPIType runs from a command to the operation it calls.
	CallsAPIType = "calls_api"
)

var (
	EntityTypes   = []string{OperationType, SchemaType}
	RelationTypes = []string{AcceptsType, ReturnsType, ReferencesType, ImplementedByType, CallsAPIType}
)

// DocID is the document an API's graph is filed under.
func DocID(name string) string { return "api:" + name }

// Plan is what Import would write.
type Plan struct {
	Name      string
	DocID     string
	Embed     map[string]string
	Entities  []knowledge.GraphEntity
	Relations []knowledge.GraphRelation
	spec      *Spec
	entity    map[string]int
}

func operationID(api string, o Operation) string { return "api:" + api + ":" + o.Method + " " + o.Path }
func schemaID(api, name string) string           { return "api:" + api + ":schema:" + name }

// Build plans the graph for one spec. Schemas are included when an operation
// takes or returns them, and so are the schemas those refer to, so a body's
// nested objects can be followed; definitions nothing uses are left out.
func Build(name string, spec *Spec) *Plan {
	p := &Plan{Name: name, DocID: DocID(name), Embed: map[string]string{}, spec: spec, entity: map[string]int{}}

	used := map[string]bool{}
	var visit func(string)
	visit = func(s string) {
		sc, ok := spec.Schemas[s]
		if !ok || used[s] {
			return
		}
		used[s] = true
		for _, f := range sc.Fields {
			visit(strings.TrimPrefix(f.Type, "[]"))
		}
	}
	for _, o := range spec.Operations {
		visit(o.Body)
		visit(o.Response)
	}

	names := make([]string, 0, len(used))
	for s := range used {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		sc := spec.Schemas[s]
		id := schemaID(name, s)
		text := schemaText(sc)
		p.add(knowledge.GraphEntity{ID: id, Name: s, Type: SchemaType, Description: text,
			Metadata: map[string]string{"api": name, "schema": s}, ChunkIDs: []string{id}}, text)
		for _, f := range sc.Fields {
			if ref := strings.TrimPrefix(f.Type, "[]"); used[ref] && ref != s {
				p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: schemaID(name, ref), Type: ReferencesType,
					Metadata: map[string]string{"api": name, "field": f.Name}})
			}
		}
	}

	for _, o := range spec.Operations {
		id := operationID(name, o)
		text := operationText(o, spec)
		meta := map[string]string{"api": name, "method": o.Method, "path": o.Path}
		if o.OperationID != "" {
			meta["operation_id"] = o.OperationID
		}
		p.add(knowledge.GraphEntity{ID: id, Name: o.Method + " " + o.Path, Type: OperationType, Description: text,
			Metadata: meta, ChunkIDs: []string{id}}, text)
		if used[o.Body] {
			p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: schemaID(name, o.Body), Type: AcceptsType,
				Metadata: map[string]string{"api": name}})
		}
		if used[o.Response] {
			p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: schemaID(name, o.Response), Type: ReturnsType,
				Metadata: map[string]string{"api": name}})
		}
	}
	return p
}

func (p *Plan) add(e knowledge.GraphEntity, text string) {
	p.entity[e.ID] = len(p.Entities)
	p.Entities = append(p.Entities, e)
	p.Embed[e.ID] = text
}

// annotate adds a line under an entity's first line, in its description and
// in the text it is retrieved by: retrieval shows the start of a hit.
func (p *Plan) annotate(id, line string) {
	p.Embed[id] = insertAfterFirstLine(p.Embed[id], line)
	if i, ok := p.entity[id]; ok {
		p.Entities[i].Description = insertAfterFirstLine(p.Entities[i].Description, line)
	}
}

const maxDescription = 600

func operationText(o Operation, spec *Spec) string {
	var b strings.Builder
	b.WriteString("API operation: " + o.Method + " " + o.Path)
	if o.OperationID != "" {
		b.WriteString(" — " + o.Name())
	}
	if len(o.Tags) > 0 {
		b.WriteString(" (" + strings.Join(o.Tags, ", ") + ")")
	}
	if d := strings.TrimSpace(strings.Join(strings.Fields(o.Summary+" "+o.Description), " ")); d != "" {
		b.WriteString("\n" + clip(d, maxDescription))
	}
	for _, pr := range o.Params {
		b.WriteString("\n  " + pr.In + " parameter " + pr.Name)
		if pr.Type != "" {
			b.WriteString(" " + pr.Type)
		}
		if pr.Required {
			b.WriteString(", required")
		}
		if pr.Desc != "" {
			b.WriteString(" — " + clip(pr.Desc, 200))
		}
	}
	if o.Body != "" {
		b.WriteString("\n  request body " + o.Body + fieldList(spec.Schemas[o.Body]))
	}
	if o.Response != "" {
		b.WriteString("\n  returns " + o.Response + fieldList(spec.Schemas[o.Response]))
	}
	return b.String()
}

func fieldList(s Schema) string {
	if len(s.Fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		parts = append(parts, strings.TrimSpace(f.Name+" "+f.Type))
	}
	return ": " + strings.Join(parts, ", ")
}

func schemaText(s Schema) string {
	var b strings.Builder
	b.WriteString("API schema: " + s.Name)
	if s.Desc != "" {
		b.WriteString(" — " + clip(strings.Join(strings.Fields(s.Desc), " "), maxDescription))
	}
	for _, f := range s.Fields {
		b.WriteString("\n  " + strings.TrimSpace(f.Name+" "+f.Type))
		if f.Desc != "" {
			b.WriteString(" — " + clip(strings.Join(strings.Fields(f.Desc), " "), 200))
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func insertAfterFirstLine(text, line string) string {
	first, rest, ok := strings.Cut(text, "\n")
	if !ok {
		return text + "\n" + line
	}
	return first + "\n" + line + "\n" + rest
}

// CodeSymbol is a function in a repository's code graph.
type CodeSymbol struct {
	ID       string
	Name     string
	FilePath string
	Summary  string
	Calls    []string
}

// LinkCode adds an implemented_by edge from each operation to the server
// function of the same name under codePath — gRPC-gateway's operation
// HaifyController_CreateResource is served by a CreateResource method — and
// writes the handler into the operation's text. A name found more than once
// under the path is left unlinked: an edge to the wrong one reads as the answer.
func (p *Plan) LinkCode(symbols []CodeSymbol, codePath string) (unlinked []string) {
	dir := strings.Trim(strings.TrimSpace(codePath), "/") + "/"
	byName := map[string][]CodeSymbol{}
	for _, s := range symbols {
		if codePath != "" && !strings.HasPrefix(strings.TrimPrefix(s.FilePath, "./"), dir) {
			continue
		}
		byName[s.Name] = append(byName[s.Name], s)
	}
	for _, o := range p.spec.Operations {
		hits := byName[o.Name()]
		if o.OperationID == "" || len(hits) != 1 {
			unlinked = append(unlinked, o.Method+" "+o.Path)
			continue
		}
		h := hits[0]
		id := operationID(p.Name, o)
		p.Relations = append(p.Relations, knowledge.GraphRelation{From: id, To: h.ID, Type: ImplementedByType,
			Metadata: map[string]string{"api": p.Name, "resolution": "name", "file_path": h.FilePath}})
		line := "Implemented by `" + h.Name + "` in " + h.FilePath
		if h.Summary != "" {
			line += ": " + strings.TrimSpace(h.Summary)
		}
		if !strings.HasSuffix(line, ".") {
			line += "."
		}
		if len(h.Calls) > 0 {
			calls := make([]string, len(h.Calls))
			for i, c := range h.Calls {
				calls[i] = "`" + c + "`"
			}
			line += " It calls " + strings.Join(calls, ", ") + "."
		}
		p.annotate(id, line)
	}
	sort.Strings(unlinked)
	return unlinked
}

// Command is a command line's command, as the command graph holds it.
type Command struct {
	ID   string
	Name string   // "haify gateway nfs create"
	Path []string // below the binary
}

// LinkCommands adds a calls_api edge from each command to the operation whose
// method name is spelled with the same words: `haify gateway nfs create` and
// CreateNFSGateway are both {create, nfs, gateway}. The words decide it, not
// the call in the code, because the code graph rarely records that call — 7
// calls edges from all of haify's cmd/cli, against 96 distinct client calls in
// the source — and each edge says it was matched by name. An operation two
// commands match, or a command two operations match, is left alone.
func (p *Plan) LinkCommands(cmds []Command) (linked int) {
	byWords := map[string][]Operation{}
	for _, o := range p.spec.Operations {
		if o.OperationID == "" {
			continue
		}
		k := wordKey(splitWords(o.Name()))
		byWords[k] = append(byWords[k], o)
	}
	claims := map[string][]Command{}
	for _, c := range cmds {
		if len(c.Path) == 0 {
			continue
		}
		var words []string
		for _, seg := range c.Path {
			words = append(words, strings.FieldsFunc(seg, func(r rune) bool { return r == '-' || r == '_' })...)
		}
		ops := byWords[wordKey(words)]
		if len(ops) != 1 {
			continue
		}
		oid := operationID(p.Name, ops[0])
		claims[oid] = append(claims[oid], c)
	}
	oids := make([]string, 0, len(claims))
	for oid := range claims {
		oids = append(oids, oid)
	}
	sort.Strings(oids)
	for _, oid := range oids {
		cs := claims[oid]
		if len(cs) != 1 {
			continue
		}
		p.Relations = append(p.Relations, knowledge.GraphRelation{From: cs[0].ID, To: oid, Type: CallsAPIType,
			Metadata: map[string]string{"api": p.Name, "resolution": "name"}})
		p.annotate(oid, "Called by the command `"+cs[0].Name+"`.")
		linked++
	}
	return linked
}

// splitWords splits a method name into lower-case words, keeping an acronym
// whole: CreateNFSGateway → create, nfs, gateway.
func splitWords(s string) []string {
	var words []string
	r := []rune(s)
	start := 0
	for i := 1; i <= len(r); i++ {
		boundary := i == len(r) ||
			(unicode.IsUpper(r[i]) && unicode.IsLower(r[i-1])) ||
			(unicode.IsUpper(r[i]) && i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1])) ||
			(unicode.IsDigit(r[i]) != unicode.IsDigit(r[i-1]))
		if boundary {
			words = append(words, string(r[start:i]))
			start = i
		}
	}
	return words
}

// wordKey is a word set as a key: lower case, singular, order-free.
func wordKey(words []string) string {
	set := map[string]bool{}
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") {
			w = strings.TrimSuffix(w, "s")
		}
		if w != "" {
			set[w] = true
		}
	}
	out := make([]string, 0, len(set))
	for w := range set {
		out = append(out, w)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// Stats summarizes an import.
type Stats struct {
	Operations, Schemas, Implemented, Commands int
}

// Import writes the plan. The caller purges the API's previous document first.
func Import(ctx context.Context, store *knowledge.Store, p *Plan) (Stats, error) {
	var st Stats
	meta := map[string]string{"document_id": p.DocID, "source": knowledge.SourceAPIReference, "api": p.Name}
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
		case OperationType:
			st.Operations++
		case SchemaType:
			st.Schemas++
		}
	}
	for _, r := range p.Relations {
		switch r.Type {
		case ImplementedByType:
			st.Implemented++
		case CallsAPIType:
			st.Commands++
		}
	}
	return st, nil
}
