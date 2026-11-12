package ralph

import (
	"testing"

	"mince/internal/litany"
	"mince/internal/pq"
	"mince/internal/rl"
)

func TestReplayGatedSwap(t *testing.T) {
	e := NewEngine(8)
	for i := 0; i < 8; i++ {
		swapped, err := e.Record(Turn{Action: rl.ActionInline, Reward: pq.New(0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		if swapped {
			t.Fatal("swapped on zero improvement")
		}
	}
	swapped := false
	for i := 0; i < 8; i++ {
		var err error
		swapped, err = e.Record(Turn{Action: rl.ActionTierShunt, Reward: pq.New(1, 1)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !swapped {
		t.Fatal("did not swap after improvement")
	}
	p := e.Current()
	if p.Version != 2 {
		t.Fatalf("version %d", p.Version)
	}
	if p.Score.String() != "1/2" {
		t.Fatalf("score %s", p.Score)
	}
	if p.Provenance == nil {
		t.Fatal("missing litany provenance")
	}
	if err := litany.VerifyChain([]*litany.Frame{p.Provenance}, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if !litany.Allowed(p.Provenance.CapMask, litany.CapRalph) {
		t.Fatal("ralph capability missing")
	}
}

func TestMinImprovementGate(t *testing.T) {
	e := NewEngine(1)
	swapped, err := e.Record(Turn{Action: rl.ActionInline, Reward: pq.New(1, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if swapped {
		t.Fatal("swapped below 1/20")
	}
	swapped, err = e.Record(Turn{Action: rl.ActionInline, Reward: pq.New(1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !swapped {
		t.Fatal("did not swap above 1/20")
	}
}

func TestRingCapsAt64(t *testing.T) {
	e := NewEngine(0)
	for i := 0; i < 100; i++ {
		if _, err := e.Record(Turn{Action: rl.ActionInline, Reward: pq.New(1, 2)}); err != nil {
			t.Fatal(err)
		}
	}
	if e.n != RingSize {
		t.Fatalf("ring n %d", e.n)
	}
}
