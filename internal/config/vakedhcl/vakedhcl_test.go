package vakedhcl

import (
	"strings"
	"testing"

	"mince/internal/litany"
)

func TestSampleConfig(t *testing.T) {
	f, err := Parse([]byte(SampleConfig))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "0.2.0" {
		t.Fatalf("version %s", c.Version)
	}
	cg, ok := c.Capabilities["cache_guard"]
	if !ok {
		t.Fatal("cache_guard missing")
	}
	if cg.Budget.String() != "3/4" || cg.MaxBytes != 4096 {
		t.Fatalf("capability %+v", cg)
	}
	if !litany.Allowed(cg.CapMask, litany.CapRetrieve|litany.CapTelemetry) {
		t.Fatalf("caps %#x", cg.CapMask)
	}
	if litany.Allowed(cg.CapMask, litany.CapExec) {
		t.Fatal("exec granted unexpectedly")
	}
	if c.Salience["kompress_ultra"].Model != "PeetPedro/kompress-v17" {
		t.Fatal("salience model")
	}
	if c.WireAgents["tier_shunt"].Kind != "tier_shunt" {
		t.Fatal("wire agent")
	}
	r := c.Ralph["default"]
	if r.Every != 64 || r.Replay != 64 || r.MinImprovement.String() != "1/20" {
		t.Fatalf("ralph %+v", r)
	}
	if !strings.Contains(c.Felix["shrink"].Source, "consume(c)") {
		t.Fatalf("felix source %q", c.Felix["shrink"].Source)
	}
}

func TestRejectsUnknownWireAgent(t *testing.T) {
	_, err := Decode(mustParse(t, `wire_agent "x" { kind = "root" }`))
	if err == nil {
		t.Fatal("unknown wire agent accepted")
	}
}

func TestRejectsBadBudget(t *testing.T) {
	_, err := Decode(mustParse(t, `capability "x" { allow = ["felix_eval"] budget = "3/0" }`))
	if err == nil {
		t.Fatal("zero denominator accepted")
	}
}

func TestRejectsUnterminatedHeredoc(t *testing.T) {
	if _, err := Parse([]byte("felix \"x\" {\n source = <<FELIX\nfn x() {}\n")); err == nil {
		t.Fatal("unterminated heredoc accepted")
	}
}

func mustParse(t *testing.T, src string) *File {
	t.Helper()
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return f
}
