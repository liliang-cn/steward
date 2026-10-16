package apigraph

import (
	"strings"
	"testing"
)

func load(t *testing.T) *Spec {
	t.Helper()
	spec, err := Load("testdata/haify-slice.swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func op(t *testing.T, s *Spec, method, path string) Operation {
	t.Helper()
	for _, o := range s.Operations {
		if o.Method == method && o.Path == path {
			return o
		}
	}
	t.Fatalf("no %s %s", method, path)
	return Operation{}
}

func TestParseSwagger2(t *testing.T) {
	s := load(t)
	nfs := op(t, s, "POST", "/v1/gateways/nfs")
	if nfs.Name() != "CreateNFSGateway" || nfs.Body != "v1CreateNFSGatewayRequest" || nfs.Response != "v1CreateNFSGatewayResponse" {
		t.Errorf("nfs = %+v", nfs)
	}
	del := op(t, s, "DELETE", "/v1/resources/{name}")
	if len(del.Params) != 1 || del.Params[0].Name != "name" || del.Params[0].In != "path" || !del.Params[0].Required {
		t.Errorf("delete params = %+v", del.Params)
	}
	list := op(t, s, "GET", "/v1/approvals")
	if len(list.Params) != 1 || list.Params[0].Name != "includeClosed" || list.Params[0].Type != "boolean" {
		t.Errorf("approvals params = %+v", list.Params)
	}
	req := s.Schemas["v1CreateNFSGatewayRequest"]
	types := map[string]string{}
	for _, f := range req.Fields {
		types[f.Name] = f.Type
	}
	if types["serviceIp"] != "string" || types["allowedIps"] != "[]string" {
		t.Errorf("fields = %v", types)
	}
}

func TestParseOpenAPI3(t *testing.T) {
	s, err := Parse([]byte(`
openapi: 3.0.0
info: {title: shop}
paths:
  /orders/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: integer, format: int64}}
    get:
      operationId: getOrder
      responses:
        "200": {content: {application/json: {schema: {$ref: "#/components/schemas/Order"}}}}
    put:
      operationId: updateOrder
      requestBody: {content: {application/json: {schema: {$ref: "#/components/schemas/Order"}}}}
      responses: {"204": {description: done}}
components:
  schemas:
    Order: {properties: {id: {type: integer}, lines: {type: array, items: {$ref: "#/components/schemas/Line"}}}}
    Line: {properties: {sku: {type: string}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	get := op(t, s, "GET", "/orders/{id}")
	if get.Response != "Order" || len(get.Params) != 1 || get.Params[0].Type != "integer(int64)" {
		t.Errorf("get = %+v", get)
	}
	if put := op(t, s, "PUT", "/orders/{id}"); put.Body != "Order" {
		t.Errorf("put = %+v", put)
	}
	p := Build("shop", s)
	rel := map[string]bool{}
	for _, r := range p.Relations {
		rel[r.From+" "+r.Type+" "+r.To] = true
	}
	if !rel["api:shop:schema:Order references api:shop:schema:Line"] {
		t.Errorf("nested schema not referenced: %v", rel)
	}
}

func TestBuildWritesWhatAnOperationTakes(t *testing.T) {
	p := Build("haify", load(t))
	id := "api:haify:POST /v1/gateways/nfs"
	text := p.Embed[id]
	for _, want := range []string{"POST /v1/gateways/nfs", "CreateNFSGateway", "request body v1CreateNFSGatewayRequest", "serviceIp string", "allowedIps []string"} {
		if !strings.Contains(text, want) {
			t.Errorf("operation text lacks %q:\n%s", want, text)
		}
	}
	rel := map[string]bool{}
	for _, r := range p.Relations {
		rel[r.From+" "+r.Type+" "+r.To] = true
	}
	if !rel[id+" accepts api:haify:schema:v1CreateNFSGatewayRequest"] || !rel[id+" returns api:haify:schema:v1CreateNFSGatewayResponse"] {
		t.Errorf("missing accepts/returns: %v", rel)
	}
}

func TestLinkCodeOnlyUnderTheServerPath(t *testing.T) {
	p := Build("haify", load(t))
	unlinked := p.LinkCode([]CodeSymbol{
		{ID: "f:srv", Name: "CreateNFSGateway", FilePath: "pkg/controller/server.go", Summary: "Creates an NFS gateway", Calls: []string{"validateIP"}},
		{ID: "f:gw", Name: "CreateNFSGateway", FilePath: "pkg/gateway/nfs.go"},
		{ID: "f:res1", Name: "CreateResource", FilePath: "pkg/controller/server.go"},
		{ID: "f:res2", Name: "CreateResource", FilePath: "pkg/controller/legacy.go"},
	}, "pkg/controller")
	links := map[string]string{}
	for _, r := range p.Relations {
		if r.Type == ImplementedByType {
			links[r.From] = r.To
		}
	}
	if links["api:haify:POST /v1/gateways/nfs"] != "f:srv" {
		t.Errorf("CreateNFSGateway linked to %q", links["api:haify:POST /v1/gateways/nfs"])
	}
	if _, ok := links["api:haify:POST /v1/resources"]; ok {
		t.Error("CreateResource has two candidates under the path and must stay unlinked")
	}
	if !strings.Contains(strings.Join(unlinked, ","), "POST /v1/resources") {
		t.Errorf("unlinked = %v", unlinked)
	}
	if !strings.Contains(p.Embed["api:haify:POST /v1/gateways/nfs"], "Implemented by `CreateNFSGateway` in pkg/controller/server.go: Creates an NFS gateway. It calls `validateIP`.") {
		t.Errorf("handler not written into the text:\n%s", p.Embed["api:haify:POST /v1/gateways/nfs"])
	}
}

func TestLinkCommandsByWords(t *testing.T) {
	if got := strings.Join(splitWords("CreateNFSGateway"), " "); got != "Create NFS Gateway" {
		t.Errorf("splitWords = %q", got)
	}
	p := Build("haify", load(t))
	n := p.LinkCommands([]Command{
		{ID: "cli:haify:gateway:nfs:create", Name: "haify gateway nfs create", Path: []string{"gateway", "nfs", "create"}},
		{ID: "cli:haify:gateway:list", Name: "haify gateway list", Path: []string{"gateway", "list"}},
		{ID: "cli:haify:resource:show", Name: "haify resource show", Path: []string{"resource", "show"}},
	})
	if n != 2 {
		t.Fatalf("linked %d commands, want 2 (nfs create, gateway list): %+v", n, p.Relations)
	}
	if !strings.Contains(p.Embed["api:haify:POST /v1/gateways/nfs"], "Called by the command `haify gateway nfs create`.") {
		t.Error("the operation does not name its command")
	}
}
