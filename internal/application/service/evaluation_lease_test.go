package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type evaluationLeaseStub struct {
	interfaces.EvaluationLeaseRepository
	renew func(context.Context) (bool, error)
}

func (s evaluationLeaseStub) RenewEvaluationRunLease(ctx context.Context, _ uint64, _, _ string, _ time.Time) (bool, error) {
	return s.renew(ctx)
}

func TestEvaluationHeartbeatCancelsRunOnLeaseLoss(t *testing.T) {
	for _, failure := range []error{nil, errors.New("database unavailable")} {
		ctx, cancel := context.WithCancel(context.Background())
		repo := evaluationLeaseStub{renew: func(context.Context) (bool, error) { return false, failure }}
		stop := startEvaluationHeartbeat(ctx, repo, 7, "run", "owner", time.Millisecond, cancel)
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
			t.Error("run was not cancelled after lease loss")
		}
		stop()
		cancel()
	}
}

func TestEvaluationHeartbeatStopCancelsAndJoinsRenewal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	repo := evaluationLeaseStub{renew: func(renewCtx context.Context) (bool, error) {
		close(started)
		<-renewCtx.Done()
		close(finished)
		return false, renewCtx.Err()
	}}
	stop := startEvaluationHeartbeat(ctx, repo, 7, "run", "owner", time.Millisecond, cancel)
	defer stop()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("renewal did not start")
	}
	stop()
	select {
	case <-finished:
	default:
		t.Fatal("stop returned before renewal finished")
	}
	require.NoError(t, ctx.Err(), "normal heartbeat shutdown must not cancel the run")
}
