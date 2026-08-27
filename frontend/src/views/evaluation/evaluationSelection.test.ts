import assert from 'node:assert/strict'
import test from 'node:test'

import type { EvaluationRunSummary } from '../../api/evaluation.ts'
import { toggleEvaluationRunSelection } from './evaluationSelection.ts'

function run(runID: string) {
  return { run_id: runID } as EvaluationRunSummary
}

test('keeps selected run snapshots across history pages', () => {
  const selected = new Map<string, EvaluationRunSummary>()
  assert.equal(toggleEvaluationRunSelection(selected, run('page-1-run')), 'selected')
  assert.equal(toggleEvaluationRunSelection(selected, run('page-2-run')), 'selected')
  assert.deepEqual([...selected.keys()], ['page-1-run', 'page-2-run'])
})

test('removes selections and enforces the comparison limit', () => {
  const selected = new Map<string, EvaluationRunSummary>()
  for (let index = 1; index <= 5; index += 1) {
    assert.equal(toggleEvaluationRunSelection(selected, run(`run-${index}`)), 'selected')
  }
  assert.equal(toggleEvaluationRunSelection(selected, run('run-6')), 'limit')
  assert.equal(toggleEvaluationRunSelection(selected, run('run-1')), 'removed')
  assert.equal(toggleEvaluationRunSelection(selected, run('run-6')), 'selected')
})
