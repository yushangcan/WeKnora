package evaluation

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestEvaluationObservationContext(t *testing.T) {
	base := context.Background()
	if ObserverFromContext(base) != nil || CurrentRunID(base) != "" || CurrentCaseID(base) != "" {
		t.Fatal("plain context unexpectedly contains an evaluation observation")
	}

	observerA := NewObserver("run-a", 1, "default", time.Now())
	observerB := NewObserver("run-b", 1, "default", time.Now())
	runA := WithEvaluationRun(base, observerA)
	runB := WithEvaluationRun(base, observerB)
	caseA1 := WithEvaluationCase(WithEvaluationPhase(runA, types.EvaluationPhaseEvaluation), "case-1")
	caseA2 := WithEvaluationCase(WithEvaluationPhase(runA, types.EvaluationPhaseEvaluation), "case-2")

	if CurrentRunID(caseA1) != "run-a" || CurrentCaseID(caseA1) != "case-1" {
		t.Fatalf("unexpected case A1 scope: run=%q case=%q", CurrentRunID(caseA1), CurrentCaseID(caseA1))
	}
	if CurrentCaseID(caseA2) != "case-2" || CurrentCaseID(runA) != "" {
		t.Fatalf("case derivation mutated a sibling or parent context")
	}
	if CurrentRunID(runB) != "run-b" || ObserverFromContext(runB) != observerB {
		t.Fatal("two run contexts are not isolated")
	}
	if CurrentPhase(caseA1) != types.EvaluationPhaseEvaluation {
		t.Fatalf("unexpected phase: %q", CurrentPhase(caseA1))
	}
}
