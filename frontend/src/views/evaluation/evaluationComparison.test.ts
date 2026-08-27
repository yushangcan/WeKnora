import assert from 'node:assert/strict'
import test from 'node:test'

import {
  formatEvaluationCost,
  formatEvaluationDelta,
  formatEvaluationDuration,
  formatEvaluationNumber,
  isTerminalEvaluationTaskStatus,
  localDateTimeToRFC3339,
} from './evaluationComparison.ts'

test('unavailable values remain visibly unavailable', () => {
  assert.equal(formatEvaluationNumber(null), '—')
  assert.equal(formatEvaluationCost(null, 'USD'), null)
  assert.equal(formatEvaluationDelta({ baseline: null, value: null, absolute: null, percent: null }), '—')
})

test('evaluation values use stable display precision', () => {
  assert.equal(formatEvaluationNumber(0.123456), '0.1235')
  assert.equal(formatEvaluationDuration(1500), '1.50 s')
  assert.equal(
    formatEvaluationDelta({ baseline: 0.5, value: 0.6, absolute: 0.1, percent: 20 }),
    '+0.1000 (+20.00%)',
  )
})

test('polling stops only for terminal task states', () => {
  assert.equal(isTerminalEvaluationTaskStatus(0), false)
  assert.equal(isTerminalEvaluationTaskStatus(1), false)
  assert.equal(isTerminalEvaluationTaskStatus(2), true)
  assert.equal(isTerminalEvaluationTaskStatus(3), true)
})

test('local date-time filters are converted to RFC3339', () => {
  const value = localDateTimeToRFC3339('2026-08-27T12:30')
  assert.ok(value?.endsWith('Z'))
  assert.equal(localDateTimeToRFC3339('not-a-time'), undefined)
})
