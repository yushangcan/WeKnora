import type { EvaluationRunSummary } from '@/api/evaluation'

export type EvaluationSelectionResult = 'selected' | 'removed' | 'limit'

export function toggleEvaluationRunSelection(
  selectedRuns: Map<string, EvaluationRunSummary>,
  run: EvaluationRunSummary,
  limit = 5,
): EvaluationSelectionResult {
  if (selectedRuns.has(run.run_id)) {
    selectedRuns.delete(run.run_id)
    return 'removed'
  }
  if (selectedRuns.size >= limit) return 'limit'
  selectedRuns.set(run.run_id, run)
  return 'selected'
}
