// Package ralph implements the replay-gated policy loop: a 64-turn ring is
// replayed into a candidate bandit, and the live atomic policy is swapped
// only when the candidate improves on the previous window by at least 1/20.
package ralph

import (
	"fmt"
	"sync"
	"sync/atomic"

	"mince/internal/experimental/rl"
	"mince/internal/litany"
	"mince/internal/pq"
)

const RingSize = 64

// Turn is one replayable decision.
type Turn struct {
	Action      rl.Action
	Reward      pq.RationalPQ
	RawBytes    int64
	MincedBytes int64
}

// Policy is an immutable snapshot apart from its bandit's internal lock.
type Policy struct {
	Version    uint64
	Score      pq.RationalPQ
	Bandit     *rl.BanditPQ
	Provenance *litany.Frame
}

// Engine owns the ring and the live policy pointer.
type Engine struct {
	mu             sync.Mutex
	ring           [RingSize]Turn
	n              int
	next           int
	turns          int
	replayEvery    int
	priorScore     pq.RationalPQ
	minImprovement pq.RationalPQ
	current        atomic.Pointer[Policy]
}

// NewEngine creates an engine. replayEvery <= 0 disables automatic replay.
func NewEngine(replayEvery int) *Engine {
	e := &Engine{
		replayEvery:    replayEvery,
		priorScore:     pq.New(0, 1),
		minImprovement: pq.New(1, 20),
	}
	e.current.Store(&Policy{
		Version: 1,
		Score:   pq.New(0, 1),
		Bandit:  rl.NewBanditPQ(),
	})
	return e
}

// Current returns the live policy.
func (e *Engine) Current() *Policy { return e.current.Load() }

// Record appends a turn, updates the live bandit, and runs replay when due.
func (e *Engine) Record(t Turn) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.current.Load()
	p.Bandit.Update(t.Action, t.Reward)
	e.ring[e.next] = t
	e.next = (e.next + 1) % RingSize
	if e.n < RingSize {
		e.n++
	}
	e.turns++
	if e.replayEvery > 0 && e.turns%e.replayEvery == 0 {
		return e.replayLocked()
	}
	return false, nil
}

// Replay forces a replay now.
func (e *Engine) Replay() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.replayLocked()
}

func (e *Engine) replayLocked() (bool, error) {
	if e.n == 0 {
		return false, nil
	}
	candidate := rl.NewBanditPQ()
	sum := pq.New(0, 1)
	for i := 0; i < e.n; i++ {
		idx := (e.next - e.n + i + RingSize) % RingSize
		t := e.ring[idx]
		candidate.Update(t.Action, t.Reward)
		sum = sum.Add(t.Reward)
	}
	score := sum.Div(pq.FromInt(int64(e.n)))
	if score.Sub(e.priorScore).Cmp(e.minImprovement) < 0 {
		return false, nil
	}
	old := e.current.Load()
	prev := [32]byte{}
	if old.Provenance != nil {
		prev = old.Provenance.Hash()
	}
	payload := []byte(fmt.Sprintf(`{"version":%d,"score":"%s"}`, old.Version+1, score))
	frame, err := litany.Seal(litany.KindPolicy, 0, litany.CapRalph|litany.CapTelemetry, pq.New(1, 1), prev, payload)
	if err != nil {
		return false, err
	}
	e.current.Store(&Policy{
		Version:    old.Version + 1,
		Score:      score,
		Bandit:     candidate,
		Provenance: frame,
	})
	e.priorScore = score
	return true, nil
}
