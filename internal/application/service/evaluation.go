package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	evaluationobs "github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"golang.org/x/sync/errgroup"
)

/*
corpus: pid -> content
queries: qid -> content
answers: aid -> content
qrels: qid -> pid
arels: qid -> aid
*/

// EvaluationService handles evaluation tasks for knowledge base and chat models
type EvaluationService struct {
	config               *config.Config                  // Application configuration
	dataset              interfaces.DatasetService       // Service for dataset operations
	knowledgeBaseService interfaces.KnowledgeBaseService // Service for knowledge base operations
	knowledgeService     interfaces.KnowledgeService     // Service for knowledge operations
	sessionService       interfaces.SessionService       // Service for chat sessions
	modelService         interfaces.ModelService         // Service for model operations
	evaluationRepository interfaces.EvaluationRepository // Repository for durable evaluation state
}

func NewEvaluationService(
	config *config.Config,
	dataset interfaces.DatasetService,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	sessionService interfaces.SessionService,
	modelService interfaces.ModelService,
	evaluationRepository interfaces.EvaluationRepository,
) interfaces.EvaluationService {
	return &EvaluationService{
		config:               config,
		dataset:              dataset,
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		sessionService:       sessionService,
		modelService:         modelService,
		evaluationRepository: evaluationRepository,
	}
}

// cloneEvaluationDetail prevents the API layer and background workers from
// sharing mutable pointers. The stored Params clone is also isolated from the
// per-case ChatManage clones used by the evaluation pipeline.
func cloneEvaluationDetail(source *types.EvaluationDetail) *types.EvaluationDetail {
	if source == nil {
		return nil
	}
	result := *source
	if source.Task != nil {
		task := *source.Task
		result.Task = &task
	}
	if source.Params != nil {
		result.Params = source.Params.Clone()
	}
	if source.Config != nil {
		if data, err := json.Marshal(source.Config); err == nil {
			var runConfig types.EvaluationRunConfig
			if err := json.Unmarshal(data, &runConfig); err == nil {
				result.Config = &runConfig
			}
		}
	}
	if source.Metric != nil {
		metric := *source.Metric
		result.Metric = &metric
	}
	result.Result = nil
	if source.Result != nil {
		// EvaluationRunResult is a JSON response contract made of serializable
		// value types. A JSON copy keeps all nested slices and pointers detached.
		if data, err := json.Marshal(source.Result); err == nil {
			var runResult types.EvaluationRunResult
			if err := json.Unmarshal(data, &runResult); err == nil {
				result.Result = &runResult
			}
		}
	}
	return &result
}

func (e *EvaluationService) EvaluationResult(ctx context.Context, taskID string) (*types.EvaluationDetail, error) {
	logger.Info(ctx, "Start getting evaluation result")
	logger.Infof(ctx, "Task ID: %s", taskID)

	tenantID := types.MustTenantIDFromContext(ctx)
	detail, err := e.evaluationRepository.GetRun(ctx, tenantID, taskID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get evaluation task: %v", err)
		return nil, err
	}

	logger.Info(ctx, "Evaluation result retrieved successfully")
	return detail, nil
}

// Evaluation starts a new evaluation task with given parameters
// datasetID: ID of the dataset to evaluate against
// knowledgeBaseID: ID of the knowledge base to use (empty to create new)
// chatModelID: ID of the chat model to evaluate
// rerankModelID: ID of the rerank model to evaluate
func (e *EvaluationService) Evaluation(ctx context.Context,
	datasetID string, knowledgeBaseID string, chatModelID string, rerankModelID string,
) (*types.EvaluationDetail, error) {
	logger.Info(ctx, "Start evaluation")
	logger.Infof(ctx, "Dataset ID: %s, Knowledge Base ID: %s, Chat Model ID: %s, Rerank Model ID: %s",
		datasetID, knowledgeBaseID, chatModelID, rerankModelID)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Tenant ID: %d", tenantID)

	if datasetID == "" {
		datasetID = defaultDatasetID
		logger.Info(ctx, "Using default dataset")
	}
	dataset, err := e.dataset.LoadDataset(ctx, datasetID)
	if err != nil {
		return nil, err
	}

	models, err := e.modelService.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	sourceKnowledgeBaseID := knowledgeBaseID
	sourceKB, err := e.resolveEvaluationSourceKnowledgeBase(ctx, knowledgeBaseID, models)
	if err != nil {
		return nil, err
	}

	if rerankModelID == "" {
		rerankModelID = selectEvaluationModelID(models, types.ModelTypeRerank)
		if rerankModelID == "" {
			logger.Warnf(ctx, "No rerank model found, skipping rerank")
		} else {
			logger.Infof(ctx, "Using default rerank model: %s", rerankModelID)
		}
	}

	if chatModelID == "" {
		chatModelID = selectEvaluationModelID(models, types.ModelTypeKnowledgeQA)
		if chatModelID == "" {
			return nil, fmt.Errorf("no default chat model found")
		}
		logger.Infof(ctx, "Using default chat model: %s", chatModelID)
	}

	temporaryKB, err := e.createEvaluationKnowledgeBase(ctx, sourceKB, chatModelID)
	if err != nil {
		return nil, err
	}
	knowledgeBaseID = temporaryKB.ID

	embeddingModel, err := e.modelService.GetModelByID(ctx, temporaryKB.EmbeddingModelID)
	if err != nil {
		_ = e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKB.ID)
		return nil, err
	}
	chatModel, err := e.modelService.GetModelByID(ctx, chatModelID)
	if err != nil {
		_ = e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKB.ID)
		return nil, err
	}
	var rerankModel *types.Model
	if rerankModelID != "" {
		rerankModel, err = e.modelService.GetModelByID(ctx, rerankModelID)
		if err != nil {
			_ = e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKB.ID)
			return nil, err
		}
	}

	// Create evaluation task with unique ID
	logger.Info(ctx, "Creating evaluation task")
	taskID := utils.GenerateTaskID("evaluation", tenantID, datasetID)
	logger.Infof(ctx, "Generated task ID: %s", taskID)

	caseConcurrency := max(runtime.GOMAXPROCS(0)-1, 1)
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID:        taskID,
			TenantID:  tenantID,
			DatasetID: datasetID,
			Status:    types.EvaluationStatuePending,
			StartTime: time.Now(),
		},
		Params: &types.ChatManage{
			PipelineRequest: types.PipelineRequest{
				VectorThreshold:  e.config.Conversation.VectorThreshold,
				KeywordThreshold: e.config.Conversation.KeywordThreshold,
				EmbeddingTopK:    e.config.Conversation.EmbeddingTopK,
				MaxRounds:        e.config.Conversation.MaxRounds,
				RerankModelID:    rerankModelID,
				RerankTopK:       e.config.Conversation.RerankTopK,
				RerankThreshold:  e.config.Conversation.RerankThreshold,
				ChatModelID:      chatModelID,
				SummaryConfig: types.SummaryConfig{
					MaxTokens:           e.config.Conversation.Summary.MaxTokens,
					RepeatPenalty:       e.config.Conversation.Summary.RepeatPenalty,
					TopK:                e.config.Conversation.Summary.TopK,
					TopP:                e.config.Conversation.Summary.TopP,
					Prompt:              e.config.Conversation.Summary.Prompt,
					ContextTemplate:     e.config.Conversation.Summary.ContextTemplate,
					FrequencyPenalty:    e.config.Conversation.Summary.FrequencyPenalty,
					PresencePenalty:     e.config.Conversation.Summary.PresencePenalty,
					NoMatchPrefix:       e.config.Conversation.Summary.NoMatchPrefix,
					Temperature:         e.config.Conversation.Summary.Temperature,
					Seed:                e.config.Conversation.Summary.Seed,
					MaxCompletionTokens: e.config.Conversation.Summary.MaxCompletionTokens,
				},
				FallbackResponse:    e.config.Conversation.FallbackResponse,
				RewritePromptSystem: e.config.Conversation.RewritePromptSystem,
				RewritePromptUser:   e.config.Conversation.RewritePromptUser,
			},
		},
	}
	detail.Task.Total = len(dataset.Cases)
	detail.Config, err = evaluationobs.NewRunConfig(
		dataset.Descriptor,
		sourceKnowledgeBaseID,
		temporaryKB,
		embeddingModel,
		chatModel,
		rerankModel,
		detail.Params,
		caseConcurrency,
		evaluationApplicationVersion(),
	)
	if err != nil {
		_ = e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKB.ID)
		return nil, err
	}
	observer := evaluationobs.NewObserver(taskID, tenantID, datasetID, detail.Task.StartTime)
	detail.Result = observer.Snapshot(types.EvaluationStatuePending, nil)

	logger.Info(ctx, "Persisting evaluation task")
	if err := e.evaluationRepository.CreateRun(ctx, detail, knowledgeBaseID); err != nil {
		_ = e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKB.ID)
		return nil, fmt.Errorf("create evaluation run: %w", err)
	}
	backgroundDetail := cloneEvaluationDetail(detail)

	// Start evaluation in background goroutine
	logger.Info(ctx, "Starting evaluation in background")
	go func() {
		// Create new context with logger for background task
		newCtx := evaluationobs.WithEvaluationRun(logger.CloneContext(ctx), observer)
		logger.Infof(newCtx, "Background evaluation started for task ID: %s", taskID)

		// Update task status to running
		backgroundDetail.Task.Status = types.EvaluationStatueRunning
		backgroundDetail.Result = observer.Snapshot(types.EvaluationStatueRunning, backgroundDetail.Metric)
		if err := e.evaluationRepository.UpdateRun(newCtx, backgroundDetail); err != nil {
			observer.AddWarning(
				"persistence_failed",
				"The evaluation could not persist its running state.",
			)
			observer.Complete()
			backgroundDetail.Task.Status = types.EvaluationStatueFailed
			backgroundDetail.Task.ErrMsg = evaluationobs.SafeErrorText(
				fmt.Sprintf("persist running evaluation state: %v", err),
			)
			backgroundDetail.Result = observer.Snapshot(types.EvaluationStatueFailed, backgroundDetail.Metric)
			if saveErr := e.evaluationRepository.SaveTerminalRun(newCtx, backgroundDetail); saveErr != nil {
				logger.Errorf(newCtx, "Failed to persist evaluation startup failure: %v", saveErr)
			}
			_ = e.knowledgeBaseService.DeleteKnowledgeBase(newCtx, knowledgeBaseID)
			return
		}
		logger.Info(newCtx, "Evaluation task status set to running")

		// Execute actual evaluation
		if err := e.EvalDataset(newCtx, backgroundDetail, knowledgeBaseID, dataset); err != nil {
			observer.AddWarning(
				"evaluation_failed",
				"The evaluation stopped early; completed observations are retained.",
			)
			observer.Complete()
			backgroundDetail.Task.Status = types.EvaluationStatueFailed
			backgroundDetail.Task.ErrMsg = evaluationobs.SafeErrorMessage(err)
			backgroundDetail.Result = observer.Snapshot(types.EvaluationStatueFailed, backgroundDetail.Metric)
			if saveErr := e.evaluationRepository.SaveTerminalRun(newCtx, backgroundDetail); saveErr != nil {
				logger.Errorf(newCtx, "Failed to persist terminal evaluation failure: %v", saveErr)
			}
			logger.Errorf(newCtx, "Evaluation task failed: %v, task ID: %s", err, taskID)
			return
		}

		// Mark task as completed successfully
		logger.Infof(newCtx, "Evaluation task completed successfully, task ID: %s", taskID)
		observer.Complete()
		backgroundDetail.Task.Status = types.EvaluationStatueSuccess
		backgroundDetail.Result = observer.Snapshot(types.EvaluationStatueSuccess, backgroundDetail.Metric)
		if err := e.evaluationRepository.SaveTerminalRun(newCtx, backgroundDetail); err != nil {
			logger.Errorf(newCtx, "Failed to persist successful evaluation result: %v", err)
		}
	}()

	logger.Infof(ctx, "Evaluation task created successfully, task ID: %s", taskID)
	responseDetail := cloneEvaluationDetail(detail)
	responseDetail.Params = evaluationobs.SafeParamsSnapshot(detail.Params)
	return responseDetail, nil
}

func (e *EvaluationService) resolveEvaluationSourceKnowledgeBase(
	ctx context.Context,
	knowledgeBaseID string,
	models []*types.Model,
) (*types.KnowledgeBase, error) {
	if knowledgeBaseID != "" {
		return e.knowledgeBaseService.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
	}
	embeddingModelID := selectEvaluationModelID(models, types.ModelTypeEmbedding)
	chatModelID := selectEvaluationModelID(models, types.ModelTypeKnowledgeQA)
	if embeddingModelID == "" || chatModelID == "" {
		return nil, fmt.Errorf("no default models found for evaluation")
	}
	return &types.KnowledgeBase{
		Type:             types.KnowledgeBaseTypeDocument,
		EmbeddingModelID: embeddingModelID,
		SummaryModelID:   chatModelID,
		IndexingStrategy: types.DefaultIndexingStrategy(),
	}, nil
}

func (e *EvaluationService) createEvaluationKnowledgeBase(
	ctx context.Context,
	source *types.KnowledgeBase,
	chatModelID string,
) (*types.KnowledgeBase, error) {
	if source == nil {
		return nil, fmt.Errorf("evaluation source knowledge base is nil")
	}
	indexing := types.IndexingStrategy{
		VectorEnabled:  source.IndexingStrategy.VectorEnabled,
		KeywordEnabled: source.IndexingStrategy.KeywordEnabled,
	}
	if indexing.IsZero() {
		indexing = types.DefaultIndexingStrategy()
	}
	summaryModelID := source.SummaryModelID
	if summaryModelID == "" {
		summaryModelID = chatModelID
	}
	var vectorStoreID *string
	if source.VectorStoreID != nil {
		value := *source.VectorStoreID
		vectorStoreID = &value
	}
	return e.knowledgeBaseService.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
		Name:             "evaluation",
		Description:      "evaluation",
		Type:             types.KnowledgeBaseTypeDocument,
		IsTemporary:      true,
		ChunkingConfig:   source.ChunkingConfig,
		EmbeddingModelID: source.EmbeddingModelID,
		SummaryModelID:   summaryModelID,
		VectorStoreID:    vectorStoreID,
		IndexingStrategy: indexing,
	})
}

func selectEvaluationModelID(models []*types.Model, modelType types.ModelType) string {
	candidates := make([]*types.Model, 0)
	for _, model := range models {
		if model != nil && model.Type == modelType && model.Status == types.ModelStatusActive {
			candidates = append(candidates, model)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].IsDefault != candidates[j].IsDefault {
			return candidates[i].IsDefault
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].ID
}

func evaluationApplicationVersion() string {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// EvalDataset performs the actual evaluation of a dataset
// Processes each QA pair in parallel and records metrics
func (e *EvaluationService) EvalDataset(
	ctx context.Context,
	detail *types.EvaluationDetail,
	knowledgeBaseID string,
	dataset *types.EvaluationDataset,
) error {
	logger.Info(ctx, "Start evaluating dataset")
	logger.Infof(ctx, "Task ID: %s, Dataset ID: %s", detail.Task.ID, detail.Task.DatasetID)
	observer := evaluationobs.ObserverFromContext(ctx)
	preparationCtx := ctx
	finishPreparation := func() {}
	if observer != nil {
		preparationCtx, finishPreparation = observer.StartPhase(ctx, types.EvaluationPhasePreparation)
	}

	if dataset == nil {
		finishPreparation()
		return fmt.Errorf("evaluation dataset is nil")
	}
	qaPairs := dataset.Cases
	logger.Infof(ctx, "Dataset retrieved successfully with %d QA pairs", len(qaPairs))
	if len(dataset.Corpus) == 0 {
		finishPreparation()
		return fmt.Errorf("evaluation dataset corpus is empty")
	}

	detail.Task.Total = len(qaPairs)
	logger.Infof(ctx, "Evaluation task contains %d QA pairs", detail.Task.Total)

	// Use the complete corpus instead of limiting retrieval to relevant passages.
	passages := getPassageList(dataset.Corpus)
	logger.Infof(ctx, "Creating knowledge from %d passages", len(passages))

	// Create knowledge base from passages (sync: wait for indexing to complete before querying)
	knowledge, err := e.knowledgeService.CreateKnowledgeFromPassageSyncWithChunking(
		preparationCtx,
		knowledgeBaseID,
		passages,
		"",
	)
	finishPreparation()
	if err != nil {
		logger.Errorf(ctx, "Failed to create knowledge from passages: %v", err)
		return err
	}
	logger.Infof(ctx, "Knowledge created and indexed successfully, ID: %s", knowledge.ID)

	// Setup cleanup of temporary resources
	defer func() {
		cleanupCtx := ctx
		finishCleanup := func() {}
		if observer != nil {
			cleanupCtx, finishCleanup = observer.StartPhase(ctx, types.EvaluationPhaseCleanup)
		}
		logger.Infof(cleanupCtx, "Cleaning up resources - deleting knowledge: %s", knowledge.ID)
		if err := e.knowledgeService.DeleteKnowledge(cleanupCtx, knowledge.ID); err != nil {
			logger.Errorf(cleanupCtx, "Failed to delete knowledge: %v, knowledge ID: %s", err, knowledge.ID)
			if observer != nil {
				observer.AddWarning("knowledge_cleanup_failed", "Temporary evaluation knowledge could not be deleted.")
			}
		}

		logger.Infof(cleanupCtx, "Cleaning up resources - deleting knowledge base: %s", knowledgeBaseID)
		if err := e.knowledgeBaseService.DeleteKnowledgeBase(cleanupCtx, knowledgeBaseID); err != nil {
			logger.Errorf(
				cleanupCtx,
				"Failed to delete knowledge base: %v, knowledge base ID: %s",
				err, knowledgeBaseID,
			)
			if observer != nil {
				observer.AddWarning("knowledge_base_cleanup_failed", "Temporary evaluation knowledge base could not be deleted.")
			}
		}
		finishCleanup()
		if observer != nil {
			detail.Result = observer.Snapshot(detail.Task.Status, detail.Metric)
		}
	}()

	evaluationCtx := ctx
	finishEvaluation := func() {}
	if observer != nil {
		evaluationCtx, finishEvaluation = observer.StartPhase(ctx, types.EvaluationPhaseEvaluation)
	}
	defer finishEvaluation()

	// Initialize parallel evaluation metrics
	var finished int
	var mu sync.Mutex
	var g errgroup.Group
	metricHook := NewHookMetric(len(qaPairs))

	// Set worker limit based on available CPUs
	g.SetLimit(max(runtime.GOMAXPROCS(0)-1, 1))
	logger.Infof(ctx, "Starting evaluation with %d parallel workers", max(runtime.GOMAXPROCS(0)-1, 1))

	// Process each QA pair in parallel
	for i, qaPair := range qaPairs {
		qaPair := qaPair
		i := i
		g.Go(func() error {
			caseCtx := evaluationCtx
			finishCase := func(error) {}
			caseID := strconv.Itoa(qaPair.QID)
			if observer != nil {
				caseCtx, finishCase = observer.StartCase(evaluationCtx, caseID)
			}
			logger.Infof(caseCtx, "Processing QA pair %d, question: %s", i, qaPair.Question)

			// Prepare chat management parameters for this QA pair
			chatManage := detail.Params.Clone()
			chatManage.Query = qaPair.Question
			chatManage.RewriteQuery = qaPair.Question
			// Set knowledge base ID and search targets for this evaluation
			chatManage.KnowledgeBaseIDs = []string{knowledgeBaseID}
			chatManage.SearchTargets = types.SearchTargets{
				&types.SearchTarget{
					Type:            types.SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: knowledgeBaseID,
				},
			}

			// Execute knowledge QA pipeline
			logger.Infof(caseCtx, "Running knowledge QA for question: %s", qaPair.Question)
			caseErr := e.sessionService.KnowledgeQAByEvent(caseCtx, chatManage, types.Pipline["rag"])
			finishCase(caseErr)
			if caseErr != nil {
				logger.Errorf(caseCtx, "Failed to process question %d: %v", i, caseErr)
				return caseErr
			}

			// Record evaluation metrics
			logger.Infof(caseCtx, "Recording metrics for QA pair %d", i)
			// MetricHook writes and snapshots share the same lock. This keeps the
			// existing formulas unchanged while avoiding concurrent partial reads.
			mu.Lock()
			metricHook.recordInit(i)
			metricHook.recordQaPair(i, qaPair)
			metricHook.recordSearchResult(i, chatManage.SearchResult)
			metricHook.recordRerankResult(i, chatManage.RerankResult)
			metricHook.recordChatResponse(i, chatManage.ChatResponse)
			metricHook.recordFinish(i)

			// Update progress metrics
			finished += 1
			metricResult := metricHook.MetricResult()
			detail.Metric = metricResult
			detail.Task.Finished = finished
			finishedSnapshot := finished
			totalSnapshot := detail.Task.Total
			if observer != nil {
				detail.Result = observer.Snapshot(detail.Task.Status, metricResult)
			}
			progressSnapshot := cloneEvaluationDetail(detail)
			caseSnapshot := evaluationCaseResult(progressSnapshot.Result, caseID)
			var persistErr error
			if caseSnapshot != nil {
				persistErr = e.evaluationRepository.SaveCaseProgress(caseCtx, progressSnapshot, caseSnapshot)
			} else {
				persistErr = e.evaluationRepository.UpdateRun(caseCtx, progressSnapshot)
			}
			mu.Unlock()
			if persistErr != nil {
				return fmt.Errorf("persist evaluation case %s: %w", caseID, persistErr)
			}
			logger.Infof(caseCtx, "Updated task progress: %d/%d completed", finishedSnapshot, totalSnapshot)
			return nil
		})
	}

	// Wait for all parallel evaluations to complete
	logger.Info(ctx, "Waiting for all evaluation tasks to complete")
	if err := g.Wait(); err != nil {
		logger.Errorf(ctx, "Evaluation error: %v", err)
		return err
	}

	// Final update of evaluation metrics
	mu.Lock()
	detail.Metric = metricHook.MetricResult()
	detail.Task.Finished = finished
	if observer != nil {
		detail.Result = observer.Snapshot(detail.Task.Status, detail.Metric)
	}
	finalSnapshot := cloneEvaluationDetail(detail)
	finalPersistErr := e.evaluationRepository.UpdateRun(ctx, finalSnapshot)
	mu.Unlock()
	if finalPersistErr != nil {
		return fmt.Errorf("persist final evaluation progress: %w", finalPersistErr)
	}

	logger.Infof(ctx, "Dataset evaluation completed successfully, task ID: %s", detail.Task.ID)
	return nil
}

func evaluationCaseResult(
	result *types.EvaluationRunResult,
	caseID string,
) *types.EvaluationCaseResult {
	if result == nil {
		return nil
	}
	for i := range result.Cases {
		if result.Cases[i].CaseID == caseID {
			return &result.Cases[i]
		}
	}
	return nil
}

// getPassageList returns the complete corpus text in its validated stable order.
func getPassageList(corpus []types.EvaluationPassage) []string {
	passages := make([]string, 0, len(corpus))
	for _, passage := range corpus {
		passages = append(passages, passage.Text)
	}
	return passages
}
