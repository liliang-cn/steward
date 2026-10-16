// Package apigraph turns an OpenAPI 3 or Swagger 2 document into a graph: one
// node per operation and per schema an operation takes or returns, edges from
// each operation to those schemas, and — given the code graph and the command
// line graph — edges to the server function that implements the operation and
// from the commands that call it.
//
// objectmodel already read a spec's components.schemas, and only those: the
// data model without the interface. "Which call deletes a resource?" or "what
// does creating an NFS gateway take?" had nothing to be answered from, since
// the paths — the methods, the parameters and where each goes, the bodies — are
// what a REST API is, and none of it reached the graph.
package apigraph

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Operation is one method on one path.
type Operation struct {
	Method      string // upper case
	Path        string
	OperationID string
	Summary     string
	Description string
	Tags        []string
	Params      []Param
	Body        string // schema name of the request body, if any
	Response    string // schema name of the success response, if any
}

// Name is the operation's method name with any service prefix dropped:
// HaifyController_CreateResource is CreateResource.
func (o Operation) Name() string {
	id := o.OperationID
	if i := strings.LastIndex(id, "_"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// Param is one path, query or header parameter. Body fields are the body
// schema's.
type Param struct {
	Name     string
	In       string
	Type     string
	Required bool
	Desc     string
}

// Schema is one named definition: its fields and the schemas it refers to.
type Schema struct {
	Name   string
	Desc   string
	Fields []Field
}

// Field is one property of a schema.
type Field struct {
	Name string
	Type string // a type, "[]T", or the name of another schema
	Desc string
}

// Spec is what a document defines.
type Spec struct {
	Title      string
	Operations []Operation
	Schemas    map[string]Schema
}

// Load reads a spec from a file; JSON and YAML are both accepted.
func Load(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse reads an OpenAPI 3 or Swagger 2 document.
func Parse(b []byte) (*Spec, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return nil, fmt.Errorf("the document has no paths: not an OpenAPI or Swagger spec")
	}
	spec := &Spec{Schemas: map[string]Schema{}}
	if info, ok := doc["info"].(map[string]any); ok {
		spec.Title, _ = info["title"].(string)
	}

	defs := mapAt(doc, "definitions") // Swagger 2
	if len(defs) == 0 {
		defs = mapAt(mapAt(doc, "components"), "schemas") // OpenAPI 3
	}
	for name, raw := range defs {
		spec.Schemas[name] = schemaOf(name, raw)
	}

	pathNames := make([]string, 0, len(paths))
	for p := range paths {
		pathNames = append(pathNames, p)
	}
	sort.Strings(pathNames)
	for _, p := range pathNames {
		item, _ := paths[p].(map[string]any)
		shared := paramsOf(item["parameters"])
		for _, method := range []string{"get", "put", "post", "delete", "patch", "head", "options"} {
			raw, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			op := Operation{Method: strings.ToUpper(method), Path: p}
			op.OperationID, _ = raw["operationId"].(string)
			op.Summary, _ = raw["summary"].(string)
			op.Description, _ = raw["description"].(string)
			for _, t := range listOf(raw["tags"]) {
				if s, ok := t.(string); ok {
					op.Tags = append(op.Tags, s)
				}
			}
			for _, prm := range append(append([]map[string]any(nil), shared...), paramsOf(raw["parameters"])...) {
				in, _ := prm["in"].(string)
				if in == "body" { // Swagger 2 carries the body as a parameter
					op.Body = refName(prm["schema"])
					continue
				}
				pr := Param{In: in}
				pr.Name, _ = prm["name"].(string)
				pr.Required, _ = prm["required"].(bool)
				pr.Desc, _ = prm["description"].(string)
				pr.Type = typeOf(prm)
				if pr.Type == "" {
					pr.Type = typeOf(mapAt(prm, "schema"))
				}
				op.Params = append(op.Params, pr)
			}
			if op.Body == "" { // OpenAPI 3
				op.Body = refName(contentSchema(mapAt(raw, "requestBody")))
			}
			op.Response = successResponse(mapAt(raw, "responses"))
			spec.Operations = append(spec.Operations, op)
		}
	}
	return spec, nil
}

func successResponse(responses map[string]any) string {
	codes := make([]string, 0, len(responses))
	for c := range responses {
		if strings.HasPrefix(c, "2") {
			codes = append(codes, c)
		}
	}
	sort.Strings(codes)
	for _, c := range codes {
		r, _ := responses[c].(map[string]any)
		if name := refName(r["schema"]); name != "" { // Swagger 2
			return name
		}
		if name := refName(contentSchema(r)); name != "" { // OpenAPI 3
			return name
		}
	}
	return ""
}

func contentSchema(holder map[string]any) any {
	content := mapAt(holder, "content")
	for _, mt := range []string{"application/json", "*/*"} {
		if m := mapAt(content, mt); m != nil {
			return m["schema"]
		}
	}
	for _, v := range content {
		if m, ok := v.(map[string]any); ok {
			return m["schema"]
		}
	}
	return nil
}

func schemaOf(name string, raw any) Schema {
	m, _ := raw.(map[string]any)
	s := Schema{Name: name}
	s.Desc, _ = m["description"].(string)
	props := mapAt(m, "properties")
	names := make([]string, 0, len(props))
	for p := range props {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		pm, _ := props[p].(map[string]any)
		f := Field{Name: p, Type: typeOf(pm)}
		f.Desc, _ = pm["description"].(string)
		s.Fields = append(s.Fields, f)
	}
	return s
}

// typeOf names a schema's type: a referenced schema by name, an array as []T.
func typeOf(m map[string]any) string {
	if m == nil {
		return ""
	}
	if name := refName(m); name != "" {
		return name
	}
	t, _ := m["type"].(string)
	if t == "array" {
		return "[]" + typeOf(mapAt(m, "items"))
	}
	if f, _ := m["format"].(string); f != "" && t != "" {
		return t + "(" + f + ")"
	}
	return t
}

func refName(v any) string {
	m, _ := v.(map[string]any)
	ref, _ := m["$ref"].(string)
	if ref == "" {
		if items := mapAt(m, "items"); items != nil {
			if n := refName(items); n != "" {
				return n
			}
		}
		return ""
	}
	return ref[strings.LastIndex(ref, "/")+1:]
}

func paramsOf(v any) []map[string]any {
	var out []map[string]any
	for _, p := range listOf(v) {
		if m, ok := p.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func mapAt(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}
