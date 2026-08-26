package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestCloneEvaluationDetailReturnsDetachedSnapshot(t *testing.T) {
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID:        "evaluation-1",
			TenantID:  1,
			DatasetID: "default",
			StartTime: time.Now(),
		},
		Params: &types.ChatManage{},
		Metric: &types.MetricResult{
			RetrievalMetrics: types.RetrievalMetrics{Precision: 0.5},
		},
		Result: &types.EvaluationRunResult{
			SchemaVersion: types.EvaluationResultSchemaVersion,
			Run: types.EvaluationRunMetadata{
				RunID: "evaluation-1",
			},
			Cases: []types.EvaluationCaseResult{{CaseID: "case-1"}},
		},
	}

	first := cloneEvaluationDetail(detail)
	first.Task.Status = types.EvaluationStatueFailed
	first.Metric.RetrievalMetrics.Precision = -1
	first.Result.Cases[0].CaseID = "mutated"

	second := cloneEvaluationDetail(detail)
	if second.Task.Status == types.EvaluationStatueFailed {
		t.Fatal("task pointer leaked from cloned detail")
	}
	if second.Metric.RetrievalMetrics.Precision != 0.5 {
		t.Fatal("metric pointer leaked from cloned detail")
	}
	if second.Result.Cases[0].CaseID != "case-1" {
		t.Fatal("nested result slice leaked from cloned detail")
	}
}

type observedEvaluationRepository struct {
	interfaces.EvaluationRepository
	mu   sync.RWMutex
	runs map[string]*types.EvaluationDetail
}

func newObservedEvaluationRepository() *observedEvaluationRepository {
	return &observedEvaluationRepository{runs: make(map[string]*types.EvaluationDetail)}
}

func (r *observedEvaluationRepository) CreateRun(
	_ context.Context,
	detail *types.EvaluationDetail,
	_ string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[detail.Task.ID] = cloneEvaluationDetail(detail)
	return nil
}

func (r *observedEvaluationRepository) GetRun(
	_ context.Context,
	tenantID uint64,
	runID string,
) (*types.EvaluationDetail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	detail, ok := r.runs[runID]
	if !ok || detail.Task.TenantID != tenantID {
		return nil, errors.New("evaluation run not found")
	}
	return cloneEvaluationDetail(detail), nil
}

func (r *observedEvaluationRepository) UpdateRun(
	_ context.Context,
	detail *types.EvaluationDetail,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[detail.Task.ID]; !ok {
		return errors.New("evaluation run not found")
	}
	r.runs[detail.Task.ID] = cloneEvaluationDetail(detail)
	return nil
}

func (r *observedEvaluationRepository) SaveCaseProgress(
	ctx context.Context,
	detail *types.EvaluationDetail,
	_ *types.EvaluationCaseResult,
) error {
	return r.UpdateRun(ctx, detail)
}

func (r *observedEvaluationRepository) SaveTerminalRun(
	ctx context.Context,
	detail *types.EvaluationDetail,
) error {
	return r.UpdateRun(ctx, detail)
}

func (r *observedEvaluationRepository) MarkInterruptedRunsFailed(
	_ context.Context,
	completedAt time.Time,
	errorMessage string,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, detail := range r.runs {
		if detail.Task.Status != types.EvaluationStatuePending &&
			detail.Task.Status != types.EvaluationStatueRunning {
			continue
		}
		copy := cloneEvaluationDetail(detail)
		copy.Task.Status = types.EvaluationStatueFailed
		copy.Task.ErrMsg = errorMessage
		copy.Result.Run.Status = types.EvaluationRunStatusFailed
		copy.Result.Run.CompletedAt = &completedAt
		r.runs[id] = copy
		count++
	}
	return count, nil
}

type observedDatasetStub struct {
	interfaces.DatasetService
}

func (observedDatasetStub) GetDatasetByID(context.Context, string) ([]*types.QAPair, error) {
	return observedDatasetCases(), nil
}

func (observedDatasetStub) LoadDataset(context.Context, string) (*types.EvaluationDataset, error) {
	cases := observedDatasetCases()
	return &types.EvaluationDataset{
		Descriptor: types.EvaluationDatasetDescriptor{
			ID: "default", Version: "1", ContentFingerprint: "sha256:test",
			QueryCount: len(cases), CorpusCount: 2, CaseCount: len(cases),
			IngestionMode: types.EvaluationDatasetModePassageChunking,
		},
		Corpus: []types.EvaluationPassage{
			{PID: 0, Text: "relevant passage"},
			{PID: 1, Text: "distractor passage"},
		},
		Cases: cases,
	}, nil
}

func observedDatasetCases() []*types.QAPair {
	return []*types.QAPair{{
		QID:      1,
		Question: "question",
		PIDs:     []int{0},
		Passages: []string{"relevant passage"},
		AID:      1,
		Answer:   "answer",
	}}
}

type observedKnowledgeBaseStub struct {
	interfaces.KnowledgeBaseService
	deleted   atomic.Bool
	deleteErr error
}

func (s *observedKnowledgeBaseStub) GetKnowledgeBaseByID(
	context.Context,
	string,
) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{EmbeddingModelID: "embedding-1", SummaryModelID: "chat-1"}, nil
}

func (s *observedKnowledgeBaseStub) CreateKnowledgeBase(
	_ context.Context,
	kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	copy := *kb
	copy.ID = "evaluation-kb"
	return &copy, nil
}

func (s *observedKnowledgeBaseStub) DeleteKnowledgeBase(context.Context, string) error {
	s.deleted.Store(true)
	return s.deleteErr
}

type observedKnowledgeStub struct {
	interfaces.KnowledgeService
	deleted      atomic.Bool
	passageCount atomic.Int64
	deleteErr    error
}

func (s *observedKnowledgeStub) CreateKnowledgeFromPassageSync(
	ctx context.Context,
	_ string,
	passages []string,
	_ string,
) (*types.Knowledge, error) {
	s.passageCount.Store(int64(len(passages)))
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeEmbedding,
		ModelID:     "embedding-1",
		ModelName:   "fake-embedding",
		Operation:   types.EvaluationOperationBatchEmbed,
		CallCount:   1,
		ItemCount:   len(passages),
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, nil)
	return &types.Knowledge{
		ID:           "evaluation-knowledge",
		ParseStatus:  types.ParseStatusProcessing,
		EnableStatus: "enabled",
	}, nil
}

func (s *observedKnowledgeStub) CreateKnowledgeFromPassageSyncWithChunking(
	ctx context.Context,
	_ string,
	passages []types.EvaluationPassage,
	_ string,
) (*types.Knowledge, error) {
	s.passageCount.Store(int64(len(passages)))
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeEmbedding,
		ModelID:     "embedding-1",
		ModelName:   "fake-embedding",
		Operation:   types.EvaluationOperationBatchEmbed,
		CallCount:   1,
		ItemCount:   len(passages),
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, nil)
	return &types.Knowledge{
		ID:           "evaluation-knowledge",
		ParseStatus:  types.ParseStatusProcessing,
		EnableStatus: "enabled",
	}, nil
}

func (s *observedKnowledgeStub) DeleteKnowledge(context.Context, string) error {
	s.deleted.Store(true)
	return s.deleteErr
}

type readinessObservedKnowledgeStub struct {
	observedKnowledgeStub
	result *types.Knowledge
}

func (s *readinessObservedKnowledgeStub) CreateKnowledgeFromPassageSyncWithChunking(
	context.Context,
	string,
	[]types.EvaluationPassage,
	string,
) (*types.Knowledge, error) {
	return s.result, nil
}

type observedSessionStub struct {
	interfaces.SessionService
}

func (observedSessionStub) KnowledgeQAByEvent(
	ctx context.Context,
	chatManage *types.ChatManage,
	_ []types.EventType,
) error {
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeRerank,
		ModelID:     "rerank-1",
		ModelName:   "fake-rerank",
		Operation:   types.EvaluationOperationRerank,
		CallCount:   1,
		ItemCount:   1,
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, nil)
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeChat,
		ModelID:     "chat-1",
		ModelName:   "fake-chat",
		Operation:   types.EvaluationOperationChat,
		CallCount:   1,
		ItemCount:   1,
		UsageSource: types.EvaluationUsageSourceProviderReported,
		Usage: &types.TokenUsage{
			PromptTokens:     3,
			CompletionTokens: 2,
			TotalTokens:      5,
		},
	}, nil)
	metadata := types.JSON(`{"evaluation_pid":0}`)
	chatManage.SearchResult = []*types.SearchResult{{Content: "relevant passage", ChunkMetadata: metadata}}
	chatManage.RerankResult = []*types.SearchResult{{Content: "relevant passage", ChunkMetadata: metadata}}
	chatManage.ChatResponse = &types.ChatResponse{
		Content: "answer",
		Usage:   types.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}
	return nil
}

type countingObservedSessionStub struct {
	interfaces.SessionService
	calls atomic.Int64
}

func (s *countingObservedSessionStub) KnowledgeQAByEvent(
	ctx context.Context,
	chatManage *types.ChatManage,
	events []types.EventType,
) error {
	s.calls.Add(1)
	return observedSessionStub{}.KnowledgeQAByEvent(ctx, chatManage, events)
}

type observedModelStub struct {
	interfaces.ModelService
}

func (observedModelStub) ListModels(context.Context) ([]*types.Model, error) {
	return []*types.Model{
		{ID: "embedding-1", Name: "embedding", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive},
		{ID: "chat-1", Name: "chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		{ID: "rerank-1", Name: "rerank", Type: types.ModelTypeRerank, Status: types.ModelStatusActive},
	}, nil
}

func (observedModelStub) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	models, _ := observedModelStub{}.ListModels(ctx)
	for _, model := range models {
		if model.ID == id {
			return model, nil
		}
	}
	return nil, errors.New("model not found")
}

type twoCaseObservedDatasetStub struct {
	interfaces.DatasetService
}

func (twoCaseObservedDatasetStub) GetDatasetByID(context.Context, string) ([]*types.QAPair, error) {
	return twoCaseObservedDatasetCases(), nil
}

func (twoCaseObservedDatasetStub) LoadDataset(context.Context, string) (*types.EvaluationDataset, error) {
	cases := twoCaseObservedDatasetCases()
	return &types.EvaluationDataset{
		Descriptor: types.EvaluationDatasetDescriptor{
			ID: "default", Version: "1", ContentFingerprint: "sha256:two-cases",
			QueryCount: len(cases), CorpusCount: 1, CaseCount: len(cases),
			IngestionMode: types.EvaluationDatasetModePassageChunking,
		},
		Corpus: []types.EvaluationPassage{{PID: 0, Text: "relevant passage"}},
		Cases:  cases,
	}, nil
}

func twoCaseObservedDatasetCases() []*types.QAPair {
	return []*types.QAPair{
		{
			QID:      1,
			Question: "question one",
			PIDs:     []int{0},
			Passages: []string{"relevant passage"},
			AID:      1,
			Answer:   "answer",
		},
		{
			QID:      2,
			Question: "question two",
			PIDs:     []int{0},
			Passages: []string{"relevant passage"},
			AID:      2,
			Answer:   "answer",
		},
	}
}

type partiallyFailingObservedSessionStub struct {
	interfaces.SessionService
}

func (partiallyFailingObservedSessionStub) KnowledgeQAByEvent(
	ctx context.Context,
	chatManage *types.ChatManage,
	_ []types.EventType,
) error {
	if evaluationobs.CurrentCaseID(ctx) != "2" {
		return observedSessionStub{}.KnowledgeQAByEvent(ctx, chatManage, types.Pipline["rag"])
	}

	callErr := errors.New("simulated chat failure")
	evaluationobs.RecordModelCall(ctx, evaluationobs.ModelCallRecord{
		ModelType:   types.EvaluationModelTypeChat,
		ModelID:     "chat-1",
		ModelName:   "fake-chat",
		Operation:   types.EvaluationOperationChat,
		CallCount:   1,
		ItemCount:   1,
		UsageSource: types.EvaluationUsageSourceUnavailable,
	}, callErr)
	return callErr
}

func waitForEvaluationTerminal(
	t *testing.T,
	service interfaces.EvaluationService,
	ctx context.Context,
	taskID string,
) *types.EvaluationDetail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result, err := service.EvaluationResult(ctx, taskID)
		if err != nil {
			t.Fatalf("poll evaluation %s: %v", taskID, err)
		}
		if result.Task.Status == types.EvaluationStatueSuccess ||
			result.Task.Status == types.EvaluationStatueFailed {
			return result
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("evaluation %s did not reach a terminal state", taskID)
	return nil
}

func TestEvaluationServiceExposesFourDimensionLifecycleResult(t *testing.T) {
	kbService := &observedKnowledgeBaseStub{}
	knowledgeService := &observedKnowledgeStub{}
	evaluationRepository := newObservedEvaluationRepository()
	cfg := &config.Config{
		Conversation: &config.ConversationConfig{
			Summary: &config.SummaryConfig{Prompt: "private evaluation prompt"},
		},
	}
	service := NewEvaluationService(
		cfg,
		observedDatasetStub{},
		kbService,
		knowledgeService,
		observedSessionStub{},
		observedModelStub{},
		evaluationRepository,
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	created, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
	if err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	if created.Result == nil || created.Result.Run.Status != types.EvaluationRunStatusPending {
		t.Fatalf("created result does not contain pending observation: %#v", created.Result)
	}
	if created.Params.SummaryConfig.Prompt != "" {
		t.Fatalf("creation response contains prompt text: %#v", created.Params.SummaryConfig)
	}

	deadline := time.Now().Add(5 * time.Second)
	var result *types.EvaluationDetail
	for time.Now().Before(deadline) {
		result, err = service.EvaluationResult(ctx, created.Task.ID)
		if err != nil {
			t.Fatalf("poll evaluation: %v", err)
		}
		if result.Task.Status == types.EvaluationStatueSuccess || result.Task.Status == types.EvaluationStatueFailed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if result == nil || result.Task.Status != types.EvaluationStatueSuccess {
		t.Fatalf("evaluation did not complete successfully: %#v", result)
	}
	if result.Metric == nil || result.Result == nil {
		t.Fatalf("old and new result contracts were not both returned: %#v", result)
	}
	if result.Result.Retrieval == nil ||
		result.Result.Retrieval.Precision != result.Metric.RetrievalMetrics.Precision {
		t.Fatalf("retrieval metric was recalculated or lost: metric=%#v result=%#v", result.Metric, result.Result)
	}
	if result.Result.Answer == nil || result.Result.Answer.BLEU1 != result.Metric.GenerationMetrics.BLEU1 {
		t.Fatalf("answer metric was recalculated or lost: metric=%#v result=%#v", result.Metric, result.Result)
	}
	if result.Result.Usage.Calls.Total != 3 || result.Result.Usage.Tokens.TotalTokens != 5 {
		t.Fatalf("unexpected usage aggregate: %#v", result.Result.Usage)
	}
	if result.Result.Cost.Status != types.EvaluationCostStatusUnavailable || result.Result.Cost.Amount != nil {
		t.Fatalf("unknown cost did not stay null/unavailable: %#v", result.Result.Cost)
	}
	if len(result.Result.Cases) != 1 || result.Result.Cases[0].CaseID != "1" {
		t.Fatalf("case observation was not connected: %#v", result.Result.Cases)
	}
	evidence := result.Result.Cases[0].Evidence
	if evidence.QID != 1 || len(evidence.GroundTruthPIDs) != 1 || evidence.GroundTruthPIDs[0] != 0 ||
		len(evidence.MetricInputPIDs) != 1 || evidence.MetricInputPIDs[0] != 0 || evidence.Metrics == nil {
		t.Fatalf("case audit evidence was not connected: %#v", evidence)
	}
	for _, fingerprint := range []string{
		evidence.QuestionFingerprint,
		evidence.ReferenceAnswerFingerprint,
		evidence.GeneratedAnswerFingerprint,
	} {
		if !strings.HasPrefix(fingerprint, "sha256:") || strings.Contains(fingerprint, "question") ||
			strings.Contains(fingerprint, "answer") {
			t.Fatalf("case text was not safely fingerprinted: %q", fingerprint)
		}
	}
	if !knowledgeService.deleted.Load() || !kbService.deleted.Load() {
		t.Fatal("existing cleanup lifecycle did not run")
	}
	if knowledgeService.passageCount.Load() != 2 {
		t.Fatalf("evaluation indexed %d passages, want the complete 2-passage corpus", knowledgeService.passageCount.Load())
	}

	recreatedService := NewEvaluationService(
		cfg,
		observedDatasetStub{},
		&observedKnowledgeBaseStub{},
		&observedKnowledgeStub{},
		observedSessionStub{},
		observedModelStub{},
		evaluationRepository,
	)
	persisted, err := recreatedService.EvaluationResult(ctx, created.Task.ID)
	if err != nil {
		t.Fatalf("load evaluation after service recreation: %v", err)
	}
	if persisted.Task.Status != types.EvaluationStatueSuccess || persisted.Result == nil {
		t.Fatalf("service recreation lost terminal result: %#v", persisted)
	}
	otherTenantCtx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(8))
	if _, err := recreatedService.EvaluationResult(otherTenantCtx, created.Task.ID); err == nil {
		t.Fatal("cross-tenant evaluation lookup unexpectedly succeeded")
	}
}

func TestEvaluationServiceRejectsUnreadyKnowledgeAndCleansResources(t *testing.T) {
	tests := []struct {
		name      string
		knowledge *types.Knowledge
	}{
		{name: "missing knowledge"},
		{
			name: "failed indexing",
			knowledge: &types.Knowledge{
				ID: "failed-knowledge", ParseStatus: types.ParseStatusFailed,
				EnableStatus: "disabled", ErrorMessage: "provider-secret-detail",
			},
		},
		{
			name: "disabled index",
			knowledge: &types.Knowledge{
				ID: "disabled-knowledge", ParseStatus: types.ParseStatusProcessing,
				EnableStatus: "disabled",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kbService := &observedKnowledgeBaseStub{}
			knowledgeService := &readinessObservedKnowledgeStub{result: test.knowledge}
			sessionService := &countingObservedSessionStub{}
			service := NewEvaluationService(
				&config.Config{Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}}},
				observedDatasetStub{},
				kbService,
				knowledgeService,
				sessionService,
				observedModelStub{},
				newObservedEvaluationRepository(),
			)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

			created, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
			if err != nil {
				t.Fatalf("create evaluation: %v", err)
			}
			result := waitForEvaluationTerminal(t, service, ctx, created.Task.ID)

			if result.Task.Status != types.EvaluationStatueFailed {
				t.Fatalf("task status = %v, want failed", result.Task.Status)
			}
			if strings.Contains(result.Task.ErrMsg, "provider-secret-detail") {
				t.Fatalf("knowledge failure detail leaked into run error: %q", result.Task.ErrMsg)
			}
			if sessionService.calls.Load() != 0 {
				t.Fatalf("RAG pipeline ran before knowledge was ready: %d calls", sessionService.calls.Load())
			}
			if !kbService.deleted.Load() {
				t.Fatal("temporary knowledge base was not cleaned up")
			}
			if test.knowledge != nil && test.knowledge.ID != "" && !knowledgeService.deleted.Load() {
				t.Fatal("temporary knowledge was not cleaned up")
			}
		})
	}
}

func TestEvaluationServiceReportsCleanupFailuresWithoutChangingRunStatus(t *testing.T) {
	kbService := &observedKnowledgeBaseStub{deleteErr: errors.New("simulated knowledge base cleanup failure")}
	knowledgeService := &observedKnowledgeStub{deleteErr: errors.New("simulated knowledge cleanup failure")}
	service := NewEvaluationService(
		&config.Config{Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}}},
		observedDatasetStub{},
		kbService,
		knowledgeService,
		observedSessionStub{},
		observedModelStub{},
		newObservedEvaluationRepository(),
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	created, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
	if err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	result := waitForEvaluationTerminal(t, service, ctx, created.Task.ID)

	if result.Task.Status != types.EvaluationStatueSuccess {
		t.Fatalf("cleanup failure changed run status: %#v", result.Task)
	}
	warningCodes := make(map[string]struct{}, len(result.Result.Warnings))
	for _, warning := range result.Result.Warnings {
		warningCodes[warning.Code] = struct{}{}
	}
	for _, code := range []string{"knowledge_cleanup_failed", "knowledge_base_cleanup_failed"} {
		if _, exists := warningCodes[code]; !exists {
			t.Fatalf("cleanup warning %q is missing: %#v", code, result.Result.Warnings)
		}
	}
}

func TestEvaluationServiceKeepsConcurrentRunsAndCasesIsolated(t *testing.T) {
	cfg := &config.Config{
		Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}},
	}
	service := NewEvaluationService(
		cfg,
		twoCaseObservedDatasetStub{},
		&observedKnowledgeBaseStub{},
		&observedKnowledgeStub{},
		observedSessionStub{},
		observedModelStub{},
		newObservedEvaluationRepository(),
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	first, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
	if err != nil {
		t.Fatalf("create first evaluation: %v", err)
	}
	second, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
	if err != nil {
		t.Fatalf("create second evaluation: %v", err)
	}
	if first.Task.ID == second.Task.ID {
		t.Fatalf("concurrent runs received the same ID: %s", first.Task.ID)
	}

	results := []*types.EvaluationDetail{
		waitForEvaluationTerminal(t, service, ctx, first.Task.ID),
		waitForEvaluationTerminal(t, service, ctx, second.Task.ID),
	}
	for _, result := range results {
		if result.Task.Status != types.EvaluationStatueSuccess || result.Result == nil {
			t.Fatalf("run did not complete successfully: %#v", result)
		}
		if result.Result.Run.RunID != result.Task.ID {
			t.Fatalf("run/task identity was mixed: run=%s task=%s", result.Result.Run.RunID, result.Task.ID)
		}
		if len(result.Result.Cases) != 2 ||
			result.Result.Cases[0].CaseID != "1" || result.Result.Cases[1].CaseID != "2" {
			t.Fatalf("case identities were mixed: %#v", result.Result.Cases)
		}
		// One preparation embedding call plus one rerank and one chat call
		// for each case. A value of 10 here would reveal cross-run mixing.
		if result.Result.Usage.Calls.Total != 5 {
			t.Fatalf("run usage was mixed or lost: %#v", result.Result.Usage)
		}
		for _, observedCase := range result.Result.Cases {
			if observedCase.Usage.Calls.Total != 2 {
				t.Fatalf("case usage was mixed: %#v", observedCase)
			}
		}
	}
}

func TestEvaluationServiceRetainsPartialResultAfterCaseFailure(t *testing.T) {
	cfg := &config.Config{
		Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}},
	}
	service := NewEvaluationService(
		cfg,
		twoCaseObservedDatasetStub{},
		&observedKnowledgeBaseStub{},
		&observedKnowledgeStub{},
		partiallyFailingObservedSessionStub{},
		observedModelStub{},
		newObservedEvaluationRepository(),
	)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	created, err := service.Evaluation(ctx, "default", "source-kb", "chat-1", "rerank-1")
	if err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	result := waitForEvaluationTerminal(t, service, ctx, created.Task.ID)

	if result.Task.Status != types.EvaluationStatueFailed {
		t.Fatalf("task status = %v, want failed", result.Task.Status)
	}
	if result.Result == nil || result.Result.Run.Status != types.EvaluationRunStatusPartial {
		t.Fatalf("partial run result was not retained: %#v", result.Result)
	}
	if result.Metric == nil || result.Task.Finished != 1 {
		t.Fatalf("completed-case quality result was lost: metric=%#v finished=%d", result.Metric, result.Task.Finished)
	}
	if len(result.Result.Cases) != 2 ||
		result.Result.Cases[0].Status != types.EvaluationRunStatusSuccess ||
		result.Result.Cases[1].Status != types.EvaluationRunStatusFailed {
		t.Fatalf("case terminal states were not retained: %#v", result.Result.Cases)
	}
	if result.Result.Usage.Calls.Total != 4 || result.Result.Usage.Calls.Failed != 1 {
		t.Fatalf("partial model usage was not retained: %#v", result.Result.Usage)
	}
	if result.Result.Retrieval == nil || result.Result.Answer == nil {
		t.Fatalf("completed quality dimensions were not mapped: %#v", result.Result)
	}
	if result.Result.Cases[1].Evidence.QID != 2 ||
		result.Result.Cases[1].Evidence.FailureStage != "rag_pipeline" {
		t.Fatalf("failed case audit stage was not retained: %#v", result.Result.Cases[1].Evidence)
	}
}
