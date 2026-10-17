package ingest

import (
	"regexp"
	"strings"
	"testing"
)

var goPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:zap\.\w+|logger\.\w+|fmt\.Errorf)\s*\(\s*"([^"]{8,})"`),
}

// A message is listed with the function that prints it. The knowledge base
// had the file of a log line but not its function, and a model asked where
// the line came from named a neighbouring function in the same file.
func TestGoMessagesCarryTheirFunction(t *testing.T) {
	src := `package gateway

var errNone = fmt.Errorf("package level message here")

func (m *Manager) dumpNodeConfigs(hosts []string) error {
	if len(hosts) == 0 {
		return fmt.Errorf("no nodes to read gateway configs from")
	}
	go func() {
		m.logger.Warn("Unreadable gateway config dump")
	}()
	return nil
}

func (m *Manager) readGatewayConfig(path string) error {
	return fmt.Errorf("gateway config %s not found on %s", path, "x")
}

func (c *cache[K]) evict() { c.logger.Info("evicting entries now") }

func helper() error { return fmt.Errorf("plain function message") }
`
	got := extractMatches(src, goPatterns, 8, funcSpans("pkg/gateway/config_read.go", src))
	want := []string{
		"Unreadable gateway config dump (in Manager.dumpNodeConfigs)",
		"evicting entries now (in cache.evict)",
		"gateway config %s not found on %s (in Manager.readGatewayConfig)",
		"no nodes to read gateway configs from (in Manager.dumpNodeConfigs)",
		"package level message here",
		"plain function message (in helper)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A message printed from two functions is listed once for each.
func TestSameMessageInTwoFunctions(t *testing.T) {
	src := `package p
func a() error { return fmt.Errorf("resource not found anywhere") }
func b() error { return fmt.Errorf("resource not found anywhere") }
`
	got := extractMatches(src, goPatterns, 8, funcSpans("p.go", src))
	if len(got) != 2 || !strings.HasSuffix(got[0], "(in a)") || !strings.HasSuffix(got[1], "(in b)") {
		t.Fatalf("got %q", got)
	}
}

// A file the parser gives up on part way still names what it got through.
func TestBrokenGoFileKeepsWhatParsed(t *testing.T) {
	src := "package p\nfunc ok() error { return fmt.Errorf(\"before the break\") }\nfunc broken( {\n"
	got := extractMatches(src, goPatterns, 8, funcSpans("p.go", src))
	if len(got) != 1 || got[0] != "before the break (in ok)" {
		t.Fatalf("got %q", got)
	}
}

func TestPythonMessagesCarryTheirFunction(t *testing.T) {
	src := `import logging
log = logging.getLogger(__name__)

class Driver:
    def attach(self):
        log.error("attach failed for volume")

    async def detach(self):
        log.error("detach failed for volume")

def main():
    log.error("could not parse arguments")

log.error("module level failure")
`
	pat := []*regexp.Regexp{regexp.MustCompile(`log\.error\(\s*"([^"]+)"`)}
	got := extractMatches(src, pat, 8, funcSpans("driver.py", src))
	want := []string{
		"attach failed for volume (in Driver.attach)",
		"could not parse arguments (in main)",
		"detach failed for volume (in Driver.detach)",
		"module level failure",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Languages read without a parser get no function name rather than a guess.
func TestOtherLanguagesAreNotGuessed(t *testing.T) {
	if spans := funcSpans("drbd_main.c", "static int f(void) { return 0; }"); spans != nil {
		t.Fatalf("C got spans %v", spans)
	}
}
