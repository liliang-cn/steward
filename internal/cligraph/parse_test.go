package cligraph

import (
	"os"
	"reflect"
	"testing"
)

func page(t *testing.T, file string, path ...string) Page {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	return Page{Path: path, Help: string(b)}
}

func flagNamed(t *testing.T, c Command, long string) Flag {
	t.Helper()
	for _, f := range c.Flags {
		if f.Long == long {
			return f
		}
	}
	t.Fatalf("%v has no %s; flags: %+v", c.Path, long, c.Flags)
	return Flag{}
}

// clap 2, the layout drbd-reactorctl 1.12 prints: upper-case sections, the
// description of a long-help flag on the lines below it.
func TestParseClap2(t *testing.T) {
	root := Parse(page(t, "clap2-root.txt"))
	if root.About != "Controls a local drbd-reactor daemon" {
		t.Errorf("about = %q", root.About)
	}
	want := []string{"cat", "disable", "edit", "enable", "evict", "ls", "restart", "rm", "start-until", "status", "generate-completion"}
	if !reflect.DeepEqual(root.Subcommands, want) {
		t.Errorf("subcommands = %v", root.Subcommands)
	}
	if f := flagNamed(t, root, "--config"); f.Short != "-c" || f.Value != "<FILE>" || f.Default != "/etc/drbd-reactor.toml" {
		t.Errorf("--config = %+v", f)
	}

	evict := Parse(page(t, "clap2-evict.txt", "evict"))
	if evict.About != "Evict a promoter plugin controlled resource" {
		t.Errorf("about = %q", evict.About)
	}
	if evict.Usage != "drbd-reactorctl evict [FLAGS] [OPTIONS] [--] [configs]..." {
		t.Errorf("usage = %q", evict.Usage)
	}
	d := flagNamed(t, evict, "--delay")
	if d.Short != "-d" || d.Value != "<delay>" || d.Default != "20" || d.Desc != "Positive number of seconds to wait for peer takeover [default: 20]" {
		t.Errorf("--delay = %+v", d)
	}
	if u := flagNamed(t, evict, "--unmask"); u.Short != "-u" || u.Value != "" ||
		u.Desc != "If set unmask targets (i.e. the equivalent of 'systemctl unmask'). This does not run any evictions. It is used to clear previous '--keep-masked' operations" {
		t.Errorf("--unmask = %+v", u)
	}
	for _, f := range evict.Flags {
		if f.Long == "--help" {
			t.Error("--help is listed as a flag")
		}
	}
	if len(evict.Args) != 1 || evict.Args[0].Name != "<configs>" || !evict.Args[0].Variadic {
		t.Errorf("args = %+v", evict.Args)
	}

	edit := Parse(page(t, "clap2-edit.txt", "edit"))
	if f := flagNamed(t, edit, "--type"); f.Default != "promoter" ||
		!reflect.DeepEqual(f.Possible, []string{"promoter", "prometheus", "agentx", "umh", "debugger"}) {
		t.Errorf("--type = %+v", f)
	}
}

// cobra, the layout haify prints: value types instead of placeholders,
// "(default …)", and a separate Global Flags section.
func TestParseCobra(t *testing.T) {
	c := Parse(page(t, "cobra-gateway-nfs-create.txt", "gateway", "nfs", "create"))
	if c.About != "Create NFS gateway" {
		t.Errorf("about = %q", c.About)
	}
	if f := flagNamed(t, c, "--fs-type"); f.Value != "string" || f.Default != "ext4" || f.Global {
		t.Errorf("--fs-type = %+v", f)
	}
	if f := flagNamed(t, c, "--allowed-ips"); f.Value != "strings" {
		t.Errorf("--allowed-ips = %+v", f)
	}
	if f := flagNamed(t, c, "--controller"); !f.Global || f.Short != "-c" || f.Default != "127.0.0.1:3374" {
		t.Errorf("--controller = %+v", f)
	}
	if f := flagNamed(t, c, "--tls"); f.Value != "" || !f.Global {
		t.Errorf("--tls = %+v", f)
	}
	g := Parse(page(t, "cobra-gateway.txt", "gateway"))
	if len(g.Subcommands) == 0 {
		t.Errorf("gateway lists no subcommands: %+v", g)
	}
}

// clap 4 and argparse put the usage on the heading's own line.
func TestParseInlineUsage(t *testing.T) {
	clap4 := Parse(Page{Path: []string{"run"}, Help: "Run a job\n\nUsage: tool run [OPTIONS] <JOB>\n\nArguments:\n  <JOB>  Job to run\n\nOptions:\n  -n, --dry-run        Print only\n      --retries <N>  Attempts [default: 3]\n  -h, --help           Print help\n"})
	if clap4.Usage != "tool run [OPTIONS] <JOB>" || clap4.About != "Run a job" {
		t.Errorf("clap4 = %+v", clap4)
	}
	if f := flagNamed(t, clap4, "--retries"); f.Value != "<N>" || f.Default != "3" {
		t.Errorf("--retries = %+v", f)
	}
	if len(clap4.Args) != 1 || clap4.Args[0].Name != "<JOB>" {
		t.Errorf("args = %+v", clap4.Args)
	}

	argparse := Parse(Page{Help: "usage: tool [-h] [--level LEVEL] path\n\nDo things.\n\npositional arguments:\n  path           where\n\noptions:\n  -h, --help     show this help message and exit\n  --level LEVEL  how much (default: 2)\n"})
	if f := flagNamed(t, argparse, "--level"); f.Value != "LEVEL" || f.Default != "2" {
		t.Errorf("--level = %+v", f)
	}
	if len(argparse.Args) != 1 || argparse.Args[0].Name != "path" {
		t.Errorf("args = %+v", argparse.Args)
	}
}
