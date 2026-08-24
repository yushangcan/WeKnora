package evaluation

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type observationContextKey struct{}

type observationScope struct {
	observer *Observer
	caseID   string
	phase    types.EvaluationPhase
}

// WithEvaluationRun creates the immutable root observation scope for one run.
func WithEvaluationRun(ctx context.Context, observer *Observer) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, observationContextKey{}, observationScope{
		observer: observer,
		phase:    types.EvaluationPhasePreparation,
	})
}

// WithEvaluationCase derives an isolated case scope. It never mutates the
// parent context, so concurrent cases cannot overwrite each other's identity.
func WithEvaluationCase(ctx context.Context, caseID string) context.Context {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return ctx
	}
	scope.caseID = caseID
	return context.WithValue(ctx, observationContextKey{}, scope)
}

// WithEvaluationPhase derives a scope for one run phase.
func WithEvaluationPhase(ctx context.Context, phase types.EvaluationPhase) context.Context {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return ctx
	}
	scope.phase = phase
	return context.WithValue(ctx, observationContextKey{}, scope)
}

// ObserverFromContext returns nil outside an observed evaluation run.
func ObserverFromContext(ctx context.Context) *Observer {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return nil
	}
	return scope.observer
}

func CurrentRunID(ctx context.Context) string {
	if observer := ObserverFromContext(ctx); observer != nil {
		return observer.RunID()
	}
	return ""
}

func CurrentCaseID(ctx context.Context) string {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return ""
	}
	return scope.caseID
}

func CurrentPhase(ctx context.Context) types.EvaluationPhase {
	scope, ok := scopeFromContext(ctx)
	if !ok {
		return ""
	}
	return scope.phase
}

func scopeFromContext(ctx context.Context) (observationScope, bool) {
	if ctx == nil {
		return observationScope{}, false
	}
	scope, ok := ctx.Value(observationContextKey{}).(observationScope)
	return scope, ok && scope.observer != nil
}
