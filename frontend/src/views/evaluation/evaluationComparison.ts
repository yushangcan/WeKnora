import type { EvaluationValueDelta } from '@/api/evaluation'

export function isTerminalEvaluationTaskStatus(status: number): boolean {
  return status >= 2
}

export function formatEvaluationNumber(value: number | null | undefined, digits = 4): string {
  if (value == null || !Number.isFinite(value)) return '—'
  return value.toFixed(digits)
}

export function formatEvaluationDuration(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '—'
  if (value < 1000) return `${Math.round(value)} ms`
  return `${(value / 1000).toFixed(2)} s`
}

export function formatEvaluationCost(
  amount: number | null | undefined,
  currency?: string,
): string | null {
  if (amount == null || !Number.isFinite(amount)) return null
  return `${currency || ''} ${amount.toFixed(6)}`.trim()
}

export function formatEvaluationDelta(delta: EvaluationValueDelta, digits = 4): string {
  if (delta.absolute == null || !Number.isFinite(delta.absolute)) return '—'
  const absolute = `${delta.absolute > 0 ? '+' : ''}${delta.absolute.toFixed(digits)}`
  if (delta.percent == null || !Number.isFinite(delta.percent)) return absolute
  const percent = `${delta.percent > 0 ? '+' : ''}${delta.percent.toFixed(2)}%`
  return `${absolute} (${percent})`
}

export function localDateTimeToRFC3339(value: string): string | undefined {
  if (!value) return undefined
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString()
}
