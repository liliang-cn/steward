package ingest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

// Which function a message is printed from. A message found with its file
// only sends an operator to the right file and leaves the function to a guess,
// and a model asked "where does this log line come from" guesses a plausible
// name rather than the real one. So every mined message carries the function
// around it, where that can be known exactly:
//
//   - Go is parsed (go/parser, no type checking), and a message belongs to the
//     innermost function declaration whose body holds it; a method is named
//     Type.Method. A file that does not parse cleanly still yields the
//     declarations the parser got through.
//   - Python is read by indentation: a def owns the lines indented under it,
//     and a def inside a class is Class.method.
//
// Other languages are left without a function name rather than given one by a
// brace-counting guess: a wrong name is worse than none, because it reads as
// a fact.

// funcSpan is a function's byte range in its file.
type funcSpan struct {
	start, end int
	name       string
}

// funcSpans lists the functions in a source file, for funcAt.
func funcSpans(path, src string) []funcSpan {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return goFuncSpans(src)
	case ".py":
		return pythonFuncSpans(src)
	}
	return nil
}

// funcAt names the innermost function holding byte offset off, or "".
func funcAt(spans []funcSpan, off int) string {
	best := -1
	for i, s := range spans {
		if off >= s.start && off < s.end && (best < 0 || s.end-s.start < spans[best].end-spans[best].start) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return spans[best].name
}

func goFuncSpans(src string) []funcSpan {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if f == nil {
		return nil
	}
	var out []funcSpan
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			if t := receiverType(fd.Recv.List[0].Type); t != "" {
				name = t + "." + name
			}
		}
		out = append(out, funcSpan{
			start: fset.Position(fd.Pos()).Offset,
			end:   fset.Position(fd.End()).Offset,
			name:  name,
		})
	}
	return out
}

// receiverType is the type a method is declared on: T for T, *T, T[K] and *T[K].
func receiverType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverType(t.X)
	case *ast.IndexExpr:
		return receiverType(t.X)
	case *ast.IndexListExpr:
		return receiverType(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

var pyBlock = regexp.MustCompile(`^([ \t]*)(?:async[ \t]+)?(def|class)[ \t]+(\w+)`)

func pythonFuncSpans(src string) []funcSpan {
	type open struct {
		indent int
		kind   string
		name   string
		start  int
	}
	var out []funcSpan
	var stack []open
	closeTo := func(indent, at int) {
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.kind == "def" {
				out = append(out, funcSpan{start: top.start, end: at, name: top.name})
			}
		}
	}
	off := 0
	for _, line := range strings.SplitAfter(src, "\n") {
		body := strings.TrimSpace(line)
		if body != "" && !strings.HasPrefix(body, "#") {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			closeTo(indent, off)
			if m := pyBlock.FindStringSubmatch(line); m != nil {
				name := m[3]
				if m[2] == "def" && len(stack) > 0 && stack[len(stack)-1].kind == "class" {
					name = stack[len(stack)-1].name + "." + name
				}
				stack = append(stack, open{indent: len(m[1]), kind: m[2], name: name, start: off})
			}
		}
		off += len(line)
	}
	closeTo(0, off)
	return out
}
