package rl_test

import (
	"testing"

	"mince/internal/experimental/ralph"
	"mince/internal/experimental/rl"
	"mince/internal/pq"
)

func TestBanditShiftAndReplayGate(t *testing.T) {
	b := rl.NewBanditPQ()
	for i := 0; i < 40; i++ {
		a := b.Select()
		reward := pq.New(0, 1)
		if a == rl.ActionInline {
			reward = pq.New(1, 1)
		}
		b.Update(a, reward)
	}
	if got := b.Best(); got != rl.ActionInline {
		t.Fatalf("best %v", got)
	}

	e := ralph.NewEngine(8)
	for i := 0; i < 16; i++ {
		reward := pq.New(0, 1)
		if i%2 == 0 {
			reward = pq.New(1, 1)
		}
		if _, err := e.Record(ralph.Turn{Action: rl.ActionInline, Reward: reward}); err != nil {
			t.Fatal(err)
		}
	}
	if e.Current().Provenance == nil {
		t.Fatal("replay did not swap policy")
	}
}
