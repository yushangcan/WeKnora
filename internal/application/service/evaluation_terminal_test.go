package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type terminalEvaluationRepositoryStub struct {
	interfaces.EvaluationRepository
	attempts       int
	failuresBefore int
	updateAttempts int
	updateErr      error
}

func (s *terminalEvaluationRepositoryStub) SaveTerminalRun(
	_ context.Context,
	_ *types.EvaluationDetail,
) error {
	s.attempts++
	if s.attempts <= s.failuresBefore {
		return errors.New("temporary persistence failure")
	}
	return nil
}

func (s *terminalEvaluationRepositoryStub) UpdateRun(
	_ context.Context,
	_ *types.EvaluationDetail,
) error {
	s.updateAttempts++
	return s.updateErr
}

func TestSaveTerminalEvaluationRunRetriesTemporaryFailures(t *testing.T) {
	repository := &terminalEvaluationRepositoryStub{failuresBefore: 2}
	service := &EvaluationService{evaluationRepository: repository}

	if err := service.saveTerminalEvaluationRun(context.Background(), &types.EvaluationDetail{}); err != nil {
		t.Fatalf("save terminal evaluation run: %v", err)
	}
	if repository.attempts != terminalEvaluationSaveAttempts {
		t.Fatalf("terminal save attempts = %d, want %d", repository.attempts, terminalEvaluationSaveAttempts)
	}
	if repository.updateAttempts != 0 {
		t.Fatalf("terminal fallback attempts = %d, want 0", repository.updateAttempts)
	}
}

func TestSaveTerminalEvaluationRunFallsBackToRunState(t *testing.T) {
	repository := &terminalEvaluationRepositoryStub{failuresBefore: terminalEvaluationSaveAttempts}
	service := &EvaluationService{evaluationRepository: repository}

	if err := service.saveTerminalEvaluationRun(context.Background(), &types.EvaluationDetail{}); err == nil {
		t.Fatal("terminal case persistence failure was not reported")
	}
	if repository.attempts != terminalEvaluationSaveAttempts {
		t.Fatalf("terminal save attempts = %d, want %d", repository.attempts, terminalEvaluationSaveAttempts)
	}
	if repository.updateAttempts != 1 {
		t.Fatalf("terminal fallback attempts = %d, want 1", repository.updateAttempts)
	}
}
