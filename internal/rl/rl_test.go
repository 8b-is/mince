package rl

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mince/internal/pq"
)

func TestRewardFromUsage(t *testing.T) {
	if got := RewardFromUsage(100, 25, false).String(); got != "3/4" {
		t.Fatalf("clean %s", got)
	}
	if got := RewardFromUsage(100, 25, true).String(); got != "-3/2" {
		t.Fatalf("retrieve %s", got)
	}
	if got := RewardFromUsage(0, 0, false).String(); got != "0/1" {
		t.Fatalf("empty %s", got)
	}
}

func TestBanditShiftsAfterFortyTurns(t *testing.T) {
	b := NewBanditPQ(ActionRetrieve, ActionInline, ActionTierShunt, ActionPanicSplice)
	for turn := 0; turn < 40; turn++ {
		a := b.Select()
		var reward pq.RationalPQ
		if turn < 20 {
			if a == ActionInline {
				reward = pq.New(1, 1)
			} else {
				reward = pq.New(0, 1)
			}
		} else {
			if a == ActionTierShunt {
				reward = pq.New(1, 1)
			} else {
				reward = pq.New(0, 1)
			}
		}
		b.Update(a, reward)
	}
	if got := b.Best(); got != ActionTierShunt {
		t.Fatalf("best %s, means: inline=%s tier=%s", got, b.Mean(ActionInline), b.Mean(ActionTierShunt))
	}
}

func TestTrajectoryJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mince", "8bis_rl_trajectories.jsonl")
	w, err := NewTrajectoryWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	row := Trajectory{Turn: 1, Action: "inline", Reward: "3/4", RawBytes: 100, MincedBytes: 25}
	if err := w.Append(row); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatal("no row")
	}
	var got Trajectory
	if err := json.Unmarshal(sc.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != row {
		t.Fatalf("got %+v want %+v", got, row)
	}
}
