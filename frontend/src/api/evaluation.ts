import { get, post } from '@/utils/request'

export type EvaluationRunStatus = 'pending' | 'running' | 'success' | 'partial' | 'failed'

export interface EvaluationWarning {
  code: string
  message: string
}

export interface EvaluationDatasetFile {
  name: string
  fingerprint: string
  size: number
}

export interface EvaluationDatasetDescriptor {
  id: string
  version: string
  content_fingerprint: string
  files?: EvaluationDatasetFile[]
  query_count: number
  corpus_count: number
  case_count: number
  ingestion_mode: string
}

export interface EvaluationModelConfig {
  id: string
  name: string
  display_name?: string
  type: string
  source: string
  provider?: string
  interface_type?: string
  embedding_dimension?: number
  max_concurrency?: number
  endpoint_fingerprint?: string
  parameters_fingerprint: string
  updated_at: string
}

export interface EvaluationModelConfigSet {
  embedding: EvaluationModelConfig
  chat: EvaluationModelConfig
  rerank?: EvaluationModelConfig | null
}

export interface EvaluationReproducibility {
  status: string
  warnings: EvaluationWarning[]
}

export interface EvaluationRunConfig {
  schema_version: string
  dataset: EvaluationDatasetDescriptor
  source_knowledge_base_id?: string
  models: EvaluationModelConfigSet
  chunking: {
    applied: boolean
    source_unit: string
    config: Record<string, unknown>
  }
  retrieval: Record<string, number>
  generation: Record<string, unknown>
  indexing: {
    vector_enabled: boolean
    keyword_enabled: boolean
    vector_store_id?: string
  }
  runtime: {
    case_concurrency: number
    metric_version: string
    result_version: string
    application_version?: string
    commit_sha: string
    vcs_modified: boolean
    commit_available: boolean
  }
  reproducibility: EvaluationReproducibility
  config_hash: string
}

export interface EvaluationRetrievalResult {
  precision: number
  recall: number
  ndcg3: number
  ndcg10: number
  mrr: number
  map: number
}

export interface EvaluationAnswerResult {
  bleu1: number
  bleu2: number
  bleu4: number
  rouge1: number
  rouge2: number
  rougel: number
}

export interface EvaluationMetrics {
  retrieval_metrics: EvaluationRetrievalResult
  generation_metrics: EvaluationAnswerResult
}

export interface EvaluationCallCounts {
  total: number
  succeeded: number
  failed: number
  items: number
}

export interface EvaluationTokenTotals {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cache_miss_tokens: number
}

export interface EvaluationUsageResult {
  status: string
  calls: EvaluationCallCounts
  tokens: EvaluationTokenTotals
  cache_status: string
  reported_call_count: number
  unavailable_call_count: number
  by_model: Array<Record<string, unknown>>
  by_phase: Array<Record<string, unknown>>
}

export interface EvaluationCostResult {
  status: string
  source: string
  currency?: string
  amount: number | null
  pricing_version?: string
  warnings: EvaluationWarning[]
}

export interface EvaluationTimingResult {
  total_wall_time_ms: number
  preparation_ms: number
  evaluation_ms: number
  cleanup_ms: number
  case_count: number
  case_avg_ms: number
  case_min_ms: number
  case_max_ms: number
  case_p50_ms: number
  case_p95_ms: number
  model_call_cumulative_ms: number
}

export interface EvaluationCaseEvidence {
  qid: number
  question_fingerprint: string
  reference_answer_fingerprint: string
  generated_answer_fingerprint: string
  ground_truth_pids: number[]
  search_pids: number[]
  rerank_pids: number[]
  metric_input_pids: number[]
  unmapped_result_count: number
  metrics?: EvaluationMetrics
  failure_stage?: string
}

export interface EvaluationCaseResult {
  case_id: string
  status: EvaluationRunStatus
  started_at: string
  completed_at: string | null
  duration_ms: number
  usage: EvaluationUsageResult
  evidence: EvaluationCaseEvidence
  warnings: EvaluationWarning[]
}

export interface EvaluationRunProgress {
  total: number
  finished: number
  cases: {
    total: number
    pending: number
    running: number
    success: number
    partial: number
    failed: number
  }
}

export interface EvaluationRunSummary {
  run_id: string
  status: EvaluationRunStatus
  dataset: EvaluationDatasetDescriptor
  source_knowledge_base_id?: string
  config_hash: string
  config_schema_version: string
  metric_version: string
  result_version: string
  models: EvaluationModelConfigSet
  reproducibility: EvaluationReproducibility
  progress: EvaluationRunProgress
  retrieval: EvaluationRetrievalResult | null
  answer: EvaluationAnswerResult | null
  usage: EvaluationUsageResult
  cost: EvaluationCostResult
  timing: EvaluationTimingResult
  warnings: EvaluationWarning[]
  error_message?: string
  started_at: string
  completed_at: string | null
  created_at: string
  updated_at: string
}

export interface EvaluationRunPage {
  items: EvaluationRunSummary[]
  total: number
  page: number
  page_size: number
}

export interface EvaluationRunOverview {
  summary: EvaluationRunSummary
  config: EvaluationRunConfig | null
  metric?: EvaluationMetrics
}

export interface EvaluationCasePage {
  items: EvaluationCaseResult[]
  total: number
  page: number
  page_size: number
}

export interface EvaluationComparisonCompatibility {
  comparable: boolean
  reasons: string[]
  warnings: string[]
}

export interface EvaluationValueDelta {
  baseline: number | null
  value: number | null
  absolute: number | null
  percent: number | null
}

export type EvaluationQualityDeltas = Record<
  'precision' | 'recall' | 'ndcg3' | 'ndcg10' | 'mrr' | 'map' | 'bleu1' | 'bleu2' | 'bleu4' | 'rouge1' | 'rouge2' | 'rougel',
  EvaluationValueDelta
>

export type EvaluationCostDeltas = Record<
  'amount' | 'calls' | 'prompt_tokens' | 'completion_tokens' | 'total_tokens' | 'cached_tokens',
  EvaluationValueDelta
>

export type EvaluationTimingDeltas = Record<
  'total_wall_time_ms' | 'preparation_ms' | 'evaluation_ms' | 'cleanup_ms' | 'case_avg_ms' | 'case_p50_ms' | 'case_p95_ms' | 'model_call_cumulative_ms',
  EvaluationValueDelta
>

export interface EvaluationRunComparison {
  run: EvaluationRunSummary
  config: EvaluationRunConfig | null
  quality_compatibility: EvaluationComparisonCompatibility
  cost_compatibility: EvaluationComparisonCompatibility
  timing_compatibility: EvaluationComparisonCompatibility
  quality: EvaluationQualityDeltas
  cost: EvaluationCostDeltas
  timing: EvaluationTimingDeltas
}

export interface EvaluationComparison {
  baseline_id: string
  runs: EvaluationRunComparison[]
}

export interface EvaluationTask {
  id: string
  tenant_id: number
  dataset_id: string
  start_time: string
  status: number
  err_msg?: string
  total: number
  finished: number
}

export interface EvaluationRunResult {
  schema_version: string
  run: {
    run_id: string
    tenant_id: number
    dataset_id: string
    started_at: string
    completed_at: string | null
    status: EvaluationRunStatus
    pricing_version?: string
  }
  retrieval: EvaluationRetrievalResult | null
  answer: EvaluationAnswerResult | null
  usage: EvaluationUsageResult
  cost: EvaluationCostResult
  timing: EvaluationTimingResult
  cases: EvaluationCaseResult[]
  warnings: EvaluationWarning[]
}

export interface EvaluationDetail {
  task: EvaluationTask
  params: Record<string, unknown> | null
  config?: EvaluationRunConfig
  metric?: EvaluationMetrics
  result?: EvaluationRunResult
}

export interface StartEvaluationRequest {
  dataset_id: string
  knowledge_base_id: string
  chat_id: string
  rerank_id?: string
}

export interface EvaluationRunListParams {
  status?: EvaluationRunStatus
  dataset_id?: string
  config_hash?: string
  embedding_model_id?: string
  chat_model_id?: string
  rerank_model_id?: string
  started_from?: string
  started_to?: string
  page?: number
  page_size?: number
}

interface ApiResponse<T> {
  success: boolean
  data: T
}

export function startEvaluation(data: StartEvaluationRequest) {
  return post<ApiResponse<EvaluationDetail>>('/api/v1/evaluation', data)
}

export function getEvaluationTask(taskID: string) {
  return get<ApiResponse<EvaluationDetail>>('/api/v1/evaluation', {
    params: { task_id: taskID },
  })
}

export function listEvaluationRuns(params: EvaluationRunListParams) {
  return get<ApiResponse<EvaluationRunPage>>('/api/v1/evaluation/runs', { params })
}

export function getEvaluationRun(runID: string) {
  return get<ApiResponse<EvaluationRunOverview>>(`/api/v1/evaluation/runs/${encodeURIComponent(runID)}`)
}

export function listEvaluationRunCases(
  runID: string,
  params: { status?: EvaluationRunStatus; page?: number; page_size?: number },
) {
  return get<ApiResponse<EvaluationCasePage>>(
    `/api/v1/evaluation/runs/${encodeURIComponent(runID)}/cases`,
    { params },
  )
}

export function compareEvaluationRuns(baselineID: string, runIDs: string[]) {
  return get<ApiResponse<EvaluationComparison>>('/api/v1/evaluation/comparison', {
    params: {
      baseline_id: baselineID,
      run_ids: runIDs.join(','),
    },
  })
}
