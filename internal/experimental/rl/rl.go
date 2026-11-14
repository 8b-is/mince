// Package rl implements mince's exact-rational contextual bandit and its
// delayed-reward trajectory log. No floats, no external ML runtime.
package rl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"mince/internal/pq"
)

// Action is a compression action the proxy can take.
type Action uint8

const (
	ActionRetrieve Action = iota + 1
	ActionInline
	ActionTierShunt
	ActionPanicSplice
)

func (a Action) String() string {
	switch a {
	case ActionRetrieve:
		return "retrieve"
	case ActionInline:
		return "inline"
	case ActionTierShunt:
		return "tier_shunt"
	case ActionPanicSplice:
		return "panic_splice"
	default:
		return "unknown"
	}
}

// RewardFromUsage maps a completed turn to the delayed reward:
// retrieve hit -3/2, clean stream (raw-minced)/raw.
func RewardFromUsage(rawBytes, mincedBytes int64, retrieveHit bool) pq.RationalPQ {
	if retrieveHit {
		return pq.New(-3, 2)
	}
	if rawBytes <= 0 {
		return pq.New(0, 1)
	}
	return pq.New(rawBytes-mincedBytes, uint64(rawBytes))
}

type arm struct {
	action Action
	mean   pq.RationalPQ
	weight pq.RationalPQ
	last   pq.RationalPQ
	pulls  int64
}

// BanditPQ is an exact-rational UCB/EXP3 hybrid. UCB selection uses
// mean + alpha/(pulls+1); EXP3 selection uses rational weights.
type BanditPQ struct {
	mu    sync.Mutex
	arms  []arm
	total int64
	alpha pq.RationalPQ
	gamma pq.RationalPQ
	rng   uint64
}

// NewBanditPQ creates a bandit over the given actions.
func NewBanditPQ(actions ...Action) *BanditPQ {
	if len(actions) == 0 {
		actions = []Action{ActionRetrieve, ActionInline, ActionTierShunt, ActionPanicSplice}
	}
	b := &BanditPQ{alpha: pq.New(1, 2), gamma: pq.New(1, 10), rng: 0x9e3779b97f4a7c15}
	for _, a := range actions {
		b.arms = append(b.arms, arm{action: a, weight: pq.New(1, 1), mean: pq.New(0, 1), last: pq.New(0, 1)})
	}
	return b
}

// Select returns the UCB arm.
func (b *BanditPQ) Select() Action {
	b.mu.Lock()
	defer b.mu.Unlock()
	best := 0
	var bestScore pq.RationalPQ
	for i := range b.arms {
		a := &b.arms[i]
		score := a.mean.Add(b.alpha.Mul(pq.FromInt(b.total)).Div(pq.FromInt(a.pulls + 1)))
		if i == 0 || score.Cmp(bestScore) > 0 {
			best, bestScore = i, score
		}
	}
	return b.arms[best].action
}

// SelectEXP3 returns an arm sampled from rational EXP3 weights.
func (b *BanditPQ) SelectEXP3() Action {
	b.mu.Lock()
	defer b.mu.Unlock()
	total := pq.New(0, 1)
	for i := range b.arms {
		total = total.Add(b.arms[i].weight)
	}
	if total.IsZero() {
		return b.arms[0].action
	}
	r := b.rand(total)
	acc := pq.New(0, 1)
	for i := range b.arms {
		acc = acc.Add(b.arms[i].weight)
		if r.Cmp(acc) < 0 {
			return b.arms[i].action
		}
	}
	return b.arms[len(b.arms)-1].action
}

func (b *BanditPQ) rand(limit pq.RationalPQ) pq.RationalPQ {
	b.rng ^= b.rng << 13
	b.rng ^= b.rng >> 7
	b.rng ^= b.rng << 17
	r := pq.New(int64(b.rng%1_000_000), 1_000_000)
	return r.Mul(limit)
}

// Update applies a delayed reward to an arm.
func (b *BanditPQ) Update(a Action, reward pq.RationalPQ) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.arms {
		arm := &b.arms[i]
		if arm.action != a {
			continue
		}
		n := arm.pulls
		old := arm.mean.Mul(pq.FromInt(n))
		arm.mean = old.Add(reward).Div(pq.FromInt(n + 1))
		arm.pulls = n + 1
		arm.last = reward
		b.total++
		// EXP3-PQ weight update: w *= 1 + gamma*reward/(pulls+1).
		factor := pq.New(1, 1).Add(b.gamma.Mul(reward).Div(pq.FromInt(arm.pulls)))
		if factor.Sign() <= 0 {
			factor = pq.New(1, 1000)
		}
		arm.weight = arm.weight.Mul(factor)
		if arm.weight.Sign() <= 0 {
			arm.weight = pq.New(1, 1000)
		}
		return
	}
}

// Best returns the highest-mean arm.
func (b *BanditPQ) Best() Action {
	b.mu.Lock()
	defer b.mu.Unlock()
	best := 0
	for i := 1; i < len(b.arms); i++ {
		c := b.arms[i].mean.Cmp(b.arms[best].mean)
		if c > 0 || (c == 0 && b.arms[i].last.Cmp(b.arms[best].last) > 0) {
			best = i
		}
	}
	return b.arms[best].action
}

// Mean returns an arm's mean reward.
func (b *BanditPQ) Mean(a Action) pq.RationalPQ {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.arms {
		if b.arms[i].action == a {
			return b.arms[i].mean
		}
	}
	return pq.New(0, 1)
}

// Trajectory is one JSONL row.
type Trajectory struct {
	Turn        int    `json:"turn"`
	Action      string `json:"action"`
	Reward      string `json:"reward"`
	RawBytes    int64  `json:"raw_bytes"`
	MincedBytes int64  `json:"minced_bytes"`
	Retrieved   bool   `json:"retrieved"`
}

// TrajectoryWriter appends rows to .mince/8bis_rl_trajectories.jsonl.
type TrajectoryWriter struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// NewTrajectoryWriter opens (creating parent 0700, file 0600) the log.
func NewTrajectoryWriter(path string) (*TrajectoryWriter, error) {
	if path == "" {
		path = filepath.Join(".mince", "8bis_rl_trajectories.jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &TrajectoryWriter{f: f, path: path}, nil
}

// Append writes one row.
func (w *TrajectoryWriter) Append(t Trajectory) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return fmt.Errorf("rl: writer closed")
	}
	enc := json.NewEncoder(w.f)
	return enc.Encode(t)
}

// Path returns the log path.
func (w *TrajectoryWriter) Path() string { return w.path }

// Close closes the log.
func (w *TrajectoryWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
