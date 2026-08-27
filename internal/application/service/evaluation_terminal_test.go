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

func TestSaveTerminalEvaluationRunRetriesTemporaryFailures(t *testing.T) {
	repository := &terminalEvaluationRepositoryStub{failuresBefore: 2}
	service := &EvaluationService{evaluationRepository: repository}

	if err := service.saveTerminalEvaluationRun(context.Background(), &types.EvaluationDetail{}); err != nil {
		t.Fatalf("save terminal evaluation run: %v", err)
	}
	if repository.attempts != terminalEvaluationSaveAttempts {
		t.Fatalf("terminal save attempts = %d, want %d", repository.attempts, terminalEvaluationSaveAttempts)
	}
}
