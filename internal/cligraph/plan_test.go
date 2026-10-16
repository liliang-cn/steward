package cligraph

import (
	"strings"
	"testing"
)

func evictPlan(t *testing.T) *Plan {
	t.Helper()
	return Build("drbd-reactorctl", []Command{
		Parse(page(t, "clap2-root.txt")),
		Parse(page(t, "clap2-evict.txt", "evict")),
		Parse(page(t, "clap2-edit.txt", "edit")),
		{Path: []string{"start-until"}, About: "Start reactor target until specified service in start list"},
		{Path: []string{"status"}, About: "Status informatin for a plugin"},
	})
}

func TestBuildGivesEachFlagItsOwnNode(t *testing.T) {
	p := evictPlan(t)
	id := "cli:drbd-reactorctl:evict:--delay"
	var found bool
	for _, e := range p.Entities {
		if e.ID != id {
			continue
		}
		found = true
		if e.Type != FlagType || e.Metadata["default"] != "20" || e.Metadata["short"] != "-d" || e.Metadata["value"] != "<delay>" {
			t.Errorf("--delay node = %+v", e)
		}
	}
	if !found {
		t.Fatalf("no node %s", id)
	}
	text := p.Embed[id]
	for _, want := range []string{"drbd-reactorctl evict", "-d, --delay <delay>", "peer takeover", "20"} {
		if !strings.Contains(text, want) {
			t.Errorf("flag text %q lacks %q", text, want)
		}
	}
	if !strings.Contains(p.Embed["cli:drbd-reactorctl:evict"], "--keep-masked") {
		t.Error("the command's text does not list its flags")
	}

	rel := map[string]bool{}
	for _, r := range p.Relations {
		rel[r.From+" "+r.Type+" "+r.To] = true
	}
	for _, want := range []string{
		"cli:drbd-reactorctl has_subcommand cli:drbd-reactorctl:evict",
		"cli:drbd-reactorctl:evict has_flag cli:drbd-reactorctl:evict:--delay",
		"cli:drbd-reactorctl:evict has_argument cli:drbd-reactorctl:evict:<configs>",
	} {
		if !rel[want] {
			t.Errorf("missing edge %s", want)
		}
	}
	// ls was listed by the root but its page was not in the walk: no edge to a node that does not exist.
	if rel["cli:drbd-reactorctl has_subcommand cli:drbd-reactorctl:ls"] {
		t.Error("edge to a command whose page was never walked")
	}
}

func TestLinkFindsTheHandlerInTheBinarysOwnFile(t *testing.T) {
	p := evictPlan(t)
	unlinked := p.Link([]CodeSymbol{
		{ID: "f:main", Name: "main", FilePath: "src/bin/drbd-reactorctl.rs"},
		{ID: "f:evict", Name: "evict", FilePath: "src/bin/drbd-reactorctl.rs", Summary: "Handles the evict subcommand", Calls: []string{"evict_resources", "evict_unmask_and_start"}},
		{ID: "f:start_until", Name: "start_until", FilePath: "src/bin/drbd-reactorctl.rs"},
		{ID: "f:daemon-main", Name: "main", FilePath: "src/main.rs"},
		// a status in the daemon and none in the CLI: not a link
		{ID: "f:daemon-status", Name: "status", FilePath: "src/plugin/promoter.rs"},
		// edit appears twice in the CLI's own file: ambiguous, not a link
		{ID: "f:edit", Name: "edit", FilePath: "src/bin/drbd-reactorctl.rs"},
		{ID: "f:Edit", Name: "Edit", FilePath: "src/bin/drbd-reactorctl.rs"},
	})
	links := map[string]string{}
	for _, r := range p.Relations {
		if r.Type == ImplementedByType {
			links[r.From] = r.To
			if r.Metadata["resolution"] != "name" {
				t.Errorf("link %s carries no resolution", r.From)
			}
		}
	}
	want := map[string]string{
		"cli:drbd-reactorctl":             "f:main",
		"cli:drbd-reactorctl:evict":       "f:evict",
		"cli:drbd-reactorctl:start-until": "f:start_until",
	}
	for from, to := range want {
		if links[from] != to {
			t.Errorf("%s implemented_by %q, want %q", from, links[from], to)
		}
	}
	if len(links) != len(want) {
		t.Errorf("links = %v", links)
	}
	if text := p.Embed["cli:drbd-reactorctl:evict"]; !strings.Contains(text, "Implemented by `evict` in src/bin/drbd-reactorctl.rs: Handles the evict subcommand. It calls `evict_resources`, `evict_unmask_and_start`.") {
		t.Errorf("the command's text does not name its handler: %q", text)
	}
	if strings.Join(unlinked, ",") != "drbd-reactorctl edit,drbd-reactorctl status" {
		t.Errorf("unlinked = %v", unlinked)
	}
}

func TestCobraHandlerNames(t *testing.T) {
	p := Build("haify", []Command{{Path: []string{"gateway", "nfs", "create"}}})
	p.Link([]CodeSymbol{{ID: "f:1", Name: "newGatewayNfsCreateCmd", FilePath: "cmd/cli/gateway_nfs.go"}})
	if len(p.Relations) != 0 {
		t.Fatalf("linked across a file that is not named for the binary: %+v", p.Relations)
	}
	p = Build("haify", []Command{{Path: []string{"gateway", "nfs", "create"}}})
	p.CodePath = "cmd/cli"
	p.Link([]CodeSymbol{{ID: "f:1", Name: "newGatewayNfsCreateCmd", FilePath: "cmd/cli/gateway_nfs.go"}})
	if len(p.Relations) != 1 || p.Relations[0].To != "f:1" {
		t.Fatalf("--code-path did not admit cmd/cli: %+v", p.Relations)
	}
	p = Build("cli", []Command{{Path: []string{"gateway", "nfs", "create"}}})
	p.Link([]CodeSymbol{{ID: "f:1", Name: "newGatewayNfsCreateCmd", FilePath: "cmd/cli/gateway_nfs.go"}})
	if len(p.Relations) != 1 || p.Relations[0].To != "f:1" {
		t.Fatalf("cobra constructor not linked: %+v", p.Relations)
	}
}
