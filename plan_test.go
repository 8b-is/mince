package mince_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/8b-is/mince"
)

func TestPlanValidationBeforeExecution(t *testing.T) {
	for _, names := range [][]mince.StageName{nil, {"first", "missing"}, {"first", " "}} {
		called := false
		_, err := mince.MinceWithPlan(context.Background(), []byte("input"), mince.Options{}, mince.Plan{Stages: names}, mince.StageRegistry{
			"first": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
				called = true
				return in, nil
			},
		})
		if err == nil || called {
			t.Fatalf("stages %v: error=%v, executed=%v", names, err, called)
		}
	}
	_, err := mince.MinceWithPlan(context.Background(), nil, mince.Options{}, mince.DefaultPlan(), mince.StageRegistry{mince.StageDeterministic: nil})
	if err == nil {
		t.Fatal("reserved registration accepted")
	}
}

func TestPlanOrderIsolationAndSingleOriginal(t *testing.T) {
	dir := t.TempDir()
	store, err := mince.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(strings.Repeat("original ", 400))
	var calls []string
	registry := mince.StageRegistry{}
	registry["first"] = func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
		calls = append(calls, "first")
		if !bytes.Equal(in.Content, original) {
			t.Fatal("first stage did not receive original candidate")
		}
		return mince.Candidate{Content: []byte(strings.Repeat("safe ", 100)), Kind: in.Kind}, nil
	}
	registry["failure"] = func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
		calls = append(calls, "failure")
		in.Content[0] = '!'
		return in, errors.New("stage declined")
	}
	registry["last"] = func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
		calls = append(calls, "last")
		if string(in.Content) != strings.Repeat("safe ", 100) {
			t.Fatal("failed stage corrupted saved candidate")
		}
		return mince.Candidate{Content: []byte("final summary"), Kind: in.Kind}, nil
	}
	result, err := mince.MinceWithPlan(context.Background(), original, mince.Options{Store: store}, mince.Plan{Stages: []mince.StageName{"first", "failure", "last"}}, registry)
	if err != nil || result.Passthrough || !strings.HasPrefix(string(result.Minced), "final summary") {
		t.Fatalf("unexpected result: %+v, %v", result, err)
	}
	if !reflect.DeepEqual(calls, []string{"first", "failure", "last"}) {
		t.Fatalf("order: %v", calls)
	}
	raw, err := store.Get(result.CCRID)
	if err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("original retrieval: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("CCR records: %d, %v", len(files), err)
	}
}

func TestPlanCancellationKeepsPriorCandidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := mince.StageRegistry{
		"safe": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
			return mince.Candidate{Content: []byte("safe summary"), Kind: in.Kind}, nil
		},
		"cancel": func(stageCtx context.Context, in mince.Candidate) (mince.Candidate, error) {
			if stageCtx != ctx {
				t.Fatal("stage received a different context")
			}
			cancel()
			return mince.Candidate{Content: []byte("bad"), Kind: in.Kind}, nil
		},
		"later": func(context.Context, mince.Candidate) (mince.Candidate, error) {
			t.Fatal("stage ran after cancellation")
			return mince.Candidate{}, nil
		},
	}
	result, err := mince.MinceWithPlan(ctx, []byte(strings.Repeat("original ", 400)), mince.Options{Store: mince.NewMemoryStore()}, mince.Plan{Stages: []mince.StageName{"safe", "cancel", "later"}}, registry)
	if err != nil || result.Passthrough || !strings.HasPrefix(string(result.Minced), "safe summary") {
		t.Fatalf("cancellation lost safe candidate: %+v, %v", result, err)
	}
}

func TestPlanMalformedJSONPreservesSafeCandidate(t *testing.T) {
	original := []byte(`{"data":"` + strings.Repeat("x", 3000) + `"}`)
	registry := mince.StageRegistry{
		"safe": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
			return mince.Candidate{Content: []byte(`{"summary":"` + strings.Repeat("s", 200) + `"}`), Kind: in.Kind}, nil
		},
		"bad": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
			return mince.Candidate{Content: []byte(`{broken`), Kind: in.Kind}, nil
		},
	}
	result, err := mince.MinceWithPlan(context.Background(), original, mince.Options{Store: mince.NewMemoryStore()}, mince.Plan{Stages: []mince.StageName{"safe", "bad"}}, registry)
	if err != nil || result.Passthrough || !bytes.Contains(result.Minced, []byte(`"summary"`)) {
		t.Fatalf("malformed JSON displaced safe candidate: %+v, %v", result, err)
	}
}

func TestDefaultPlanMatchesMince(t *testing.T) {
	for _, content := range [][]byte{[]byte("small"), []byte(strings.Repeat("INFO repeated message\n", 300)), []byte(`{"data":"` + strings.Repeat("x", 3000) + `"}`)} {
		opts := mince.Options{Store: mince.NewMemoryStore()}
		want := mince.Mince(context.Background(), content, opts)
		got, err := mince.MinceWithPlan(context.Background(), content, opts, mince.DefaultPlan(), nil)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("default plan differs: got %+v, want %+v, err %v", got, want, err)
		}
	}
}

func TestPlanDeclinesPreserveCandidate(t *testing.T) {
	for _, name := range []string{"empty", "unchanged", "larger", "different kind"} {
		t.Run(name, func(t *testing.T) {
			registry := mince.StageRegistry{
				"safe": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
					return mince.Candidate{Content: []byte("safe summary"), Kind: in.Kind}, nil
				},
				"decline": func(_ context.Context, in mince.Candidate) (mince.Candidate, error) {
					switch name {
					case "empty":
						in.Content = nil
					case "larger":
						in.Content = bytes.Repeat([]byte("x"), 100)
					case "different kind":
						in.Content, in.Kind = []byte(`{}`), mince.KindJSON
					}
					return in, nil
				},
			}
			result, err := mince.MinceWithPlan(context.Background(), bytes.Repeat([]byte("x"), 3000), mince.Options{Store: mince.NewMemoryStore()}, mince.Plan{Stages: []mince.StageName{"safe", "decline"}}, registry)
			if err != nil || result.Passthrough || !bytes.HasPrefix(result.Minced, []byte("safe summary")) {
				t.Fatalf("decline lost candidate: %+v, %v", result, err)
			}
		})
	}
}

func TestAlreadyCancelledPlanDoesNotRunStages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := []byte(strings.Repeat("original ", 400))
	result, err := mince.MinceWithPlan(ctx, original, mince.Options{}, mince.Plan{Stages: []mince.StageName{"model"}}, mince.StageRegistry{
		"model": func(context.Context, mince.Candidate) (mince.Candidate, error) {
			t.Fatal("stage ran with cancelled context")
			return mince.Candidate{}, nil
		},
	})
	if err != nil || !result.Passthrough || result.CCRID != "" || !bytes.Equal(result.Minced, original) {
		t.Fatalf("expected original passthrough: %+v, %v", result, err)
	}
}
