<template>
  <t-drawer
    :visible="visible"
    :header="t('evaluation.comparison.title')"
    size="88%"
    :footer="false"
    destroy-on-close
    @update:visible="emit('update:visible', $event)"
  >
    <div class="comparison-toolbar">
      <div>
        <h3>{{ t('evaluation.comparison.baseline') }}</h3>
        <p>{{ t('evaluation.comparison.hint') }}</p>
      </div>
      <t-select
        v-model="baselineID"
        class="baseline-select"
        :options="baselineOptions"
        @change="loadComparison"
      />
    </div>

    <t-loading :loading="loading">
      <div v-if="comparison" class="comparison-content">
        <section class="comparison-section">
          <h3>{{ t('evaluation.comparison.configurations') }}</h3>
          <div class="configuration-grid">
            <article
              v-for="entry in comparison.runs"
              :key="entry.run.run_id"
              class="configuration-card"
              :class="{ baseline: entry.run.run_id === comparison.baseline_id }"
            >
              <div class="configuration-title">
                <strong>{{ shortID(entry.run.run_id) }}</strong>
                <t-tag v-if="entry.run.run_id === comparison.baseline_id" theme="primary" variant="light">
                  {{ t('evaluation.comparison.baselineTag') }}
                </t-tag>
              </div>
              <dl>
                <dt>{{ t('evaluation.fields.dataset') }}</dt>
                <dd>{{ entry.run.dataset.id }} · {{ shortID(entry.run.dataset.content_fingerprint) }}</dd>
                <dt>{{ t('evaluation.fields.configHash') }}</dt>
                <dd :title="entry.run.config_hash">{{ shortID(entry.run.config_hash) }}</dd>
                <dt>{{ t('evaluation.models.embedding') }}</dt>
                <dd>{{ modelLabel(entry.run.models.embedding) }}</dd>
                <dt>{{ t('evaluation.models.chat') }}</dt>
                <dd>{{ modelLabel(entry.run.models.chat) }}</dd>
                <dt>{{ t('evaluation.models.rerank') }}</dt>
                <dd>{{ modelLabel(entry.run.models.rerank) }}</dd>
              </dl>
            </article>
          </div>
        </section>

        <ComparisonDimension
          :title="t('evaluation.dimensions.retrievalAnswer')"
          :metric-keys="qualityKeys"
          :entries="comparison.runs"
          dimension="quality"
          :baseline-id="comparison.baseline_id"
        />
        <ComparisonDimension
          :title="t('evaluation.dimensions.cost')"
          :metric-keys="costKeys"
          :entries="comparison.runs"
          dimension="cost"
          :baseline-id="comparison.baseline_id"
        />
        <ComparisonDimension
          :title="t('evaluation.dimensions.timing')"
          :metric-keys="timingKeys"
          :entries="comparison.runs"
          dimension="timing"
          :baseline-id="comparison.baseline_id"
        />
      </div>
      <t-empty v-else-if="!loading" :description="t('evaluation.comparison.empty')" />
    </t-loading>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch, type PropType } from 'vue'
import { MessagePlugin, Tag as TTag } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  compareEvaluationRuns,
  type EvaluationComparison,
  type EvaluationModelConfig,
  type EvaluationRunComparison,
  type EvaluationRunSummary,
  type EvaluationValueDelta,
} from '@/api/evaluation'
import {
  formatEvaluationDelta,
  formatEvaluationDuration,
  formatEvaluationNumber,
} from '../evaluationComparison'

type ComparisonDimensionName = 'quality' | 'cost' | 'timing'
type MetricKey = { key: string; label: string }

const props = defineProps<{
  visible: boolean
  runs: EvaluationRunSummary[]
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const { t, te } = useI18n()
const loading = ref(false)
const baselineID = ref('')
const comparison = ref<EvaluationComparison | null>(null)

const baselineOptions = computed(() => props.runs.map((run) => ({
  value: run.run_id,
  label: `${shortID(run.run_id)} · ${run.dataset.id}`,
})))

const qualityKeys = computed<MetricKey[]>(() => [
  'precision', 'recall', 'ndcg3', 'ndcg10', 'mrr', 'map',
  'bleu1', 'bleu2', 'bleu4', 'rouge1', 'rouge2', 'rougel',
].map((key) => ({ key, label: t(`evaluation.metrics.${key}`) })))

const costKeys = computed<MetricKey[]>(() => [
  'amount', 'calls', 'prompt_tokens', 'completion_tokens', 'total_tokens', 'cached_tokens',
].map((key) => ({ key, label: t(`evaluation.metrics.${key}`) })))

const timingKeys = computed<MetricKey[]>(() => [
  'total_wall_time_ms', 'preparation_ms', 'evaluation_ms', 'cleanup_ms',
  'case_avg_ms', 'case_p50_ms', 'case_p95_ms', 'model_call_cumulative_ms',
].map((key) => ({ key, label: t(`evaluation.metrics.${key}`) })))

watch(
  () => [props.visible, props.runs.map((run) => run.run_id).join(',')] as const,
  ([visible]) => {
    if (!visible) return
    if (!props.runs.some((run) => run.run_id === baselineID.value)) {
      baselineID.value = props.runs[0]?.run_id || ''
    }
    void loadComparison()
  },
  { immediate: true },
)

async function loadComparison() {
  if (!props.visible || !baselineID.value || props.runs.length < 2) return
  loading.value = true
  comparison.value = null
  try {
    const response = await compareEvaluationRuns(
      baselineID.value,
      props.runs.map((run) => run.run_id),
    )
    comparison.value = response.data
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('evaluation.messages.compareFailed'))
  } finally {
    loading.value = false
  }
}

function shortID(value?: string) {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 18)}…` : value
}

function modelLabel(model?: EvaluationModelConfig | null) {
  if (!model) return t('evaluation.common.notConfigured')
  return model.display_name || model.name || model.id || '—'
}

const ComparisonDimension = defineComponent({
  name: 'ComparisonDimension',
  props: {
    title: { type: String, required: true },
    metricKeys: { type: Array as PropType<MetricKey[]>, required: true },
    entries: { type: Array as PropType<EvaluationRunComparison[]>, required: true },
    dimension: { type: String as PropType<ComparisonDimensionName>, required: true },
    baselineId: { type: String, required: true },
  },
  setup(componentProps) {
    const compatibility = (entry: EvaluationRunComparison) => entry[`${componentProps.dimension}_compatibility`]
    const delta = (entry: EvaluationRunComparison, key: string) => (
      entry[componentProps.dimension] as unknown as Record<string, EvaluationValueDelta>
    )[key]
    const valueText = (value: EvaluationValueDelta, key: string) => {
      if (componentProps.dimension === 'timing') return formatEvaluationDuration(value.value)
      if (componentProps.dimension === 'cost' && key === 'amount') return formatEvaluationNumber(value.value, 6)
      if (componentProps.dimension === 'cost') return formatEvaluationNumber(value.value, 0)
      return formatEvaluationNumber(value.value)
    }
    const compatibilityMessage = (entry: EvaluationRunComparison) => {
      const state = compatibility(entry)
      const values = [...state.reasons, ...state.warnings]
      return values.map((code) => {
        const key = `evaluation.compatibility.${code}`
        return te(key) ? t(key) : code
      }).join(' · ')
    }

    return () => h('section', { class: 'comparison-section' }, [
      h('h3', componentProps.title),
      h('div', { class: 'compatibility-row' }, componentProps.entries.map((entry) => {
        const state = compatibility(entry)
        return h('div', { class: ['compatibility-item', { incompatible: !state.comparable }] }, [
          h(TTag, {
            theme: state.comparable ? 'success' : 'warning',
            variant: 'light',
          }, { default: () => `${shortID(entry.run.run_id)} · ${state.comparable ? t('evaluation.comparison.comparable') : t('evaluation.comparison.notComparable')}` }),
          compatibilityMessage(entry) ? h('small', compatibilityMessage(entry)) : null,
        ])
      })),
      h('div', { class: 'comparison-table-wrap' }, [
        h('table', { class: 'comparison-table' }, [
          h('thead', [h('tr', [
            h('th', t('evaluation.comparison.metric')),
            ...componentProps.entries.map((entry) => h('th', [
              shortID(entry.run.run_id),
              entry.run.run_id === componentProps.baselineId
                ? h('small', t('evaluation.comparison.baselineTag'))
                : null,
            ])),
          ])]),
          h('tbody', componentProps.metricKeys.map((metric) => h('tr', [
            h('td', metric.label),
            ...componentProps.entries.map((entry) => {
              const value = delta(entry, metric.key)
              return h('td', [
                h('strong', valueText(value, metric.key)),
                h('small', formatEvaluationDelta(value, componentProps.dimension === 'cost' && metric.key === 'amount' ? 6 : componentProps.dimension === 'cost' ? 0 : 4)),
              ])
            }),
          ]))),
        ]),
      ]),
    ])
  },
})
</script>

<style scoped lang="less">
.comparison-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
  padding: 16px;
  border-radius: 10px;
  background: var(--td-bg-color-secondarycontainer);

  h3, p { margin: 0; }
  p { margin-top: 5px; color: var(--td-text-color-secondary); }
}

.baseline-select { width: min(360px, 45%); }
.comparison-content { display: flex; flex-direction: column; gap: 18px; }

.comparison-section {
  padding: 18px;
  border: 1px solid var(--td-component-border);
  border-radius: 10px;
  background: var(--td-bg-color-container);

  h3 { margin: 0 0 14px; font-size: 16px; }
}

.configuration-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 12px;
}

.configuration-card {
  padding: 14px;
  border: 1px solid var(--td-component-border);
  border-radius: 8px;

  &.baseline { border-color: var(--td-brand-color); }

  dl {
    display: grid;
    grid-template-columns: 88px minmax(0, 1fr);
    gap: 7px;
    margin: 12px 0 0;
  }
  dt { color: var(--td-text-color-placeholder); }
  dd { margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
}

.configuration-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.compatibility-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 8px;
  margin-bottom: 12px;
}

.compatibility-item {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;

  small { color: var(--td-text-color-placeholder); }
  &.incompatible small { color: var(--td-warning-color); }
}

.comparison-table-wrap { overflow-x: auto; }
.comparison-table {
  width: 100%;
  min-width: 720px;
  border-collapse: collapse;

  th, td {
    padding: 10px 12px;
    border-bottom: 1px solid var(--td-component-stroke);
    text-align: left;
  }
  th { color: var(--td-text-color-secondary); background: var(--td-bg-color-secondarycontainer); }
  th small, td small { display: block; margin-top: 3px; color: var(--td-text-color-placeholder); font-weight: 400; }
  td strong { font-variant-numeric: tabular-nums; }
}

@media (max-width: 720px) {
  .comparison-toolbar { align-items: stretch; flex-direction: column; }
  .baseline-select { width: 100%; }
}
</style>

<style lang="less">
.comparison-section {
  .compatibility-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 8px;
    margin-bottom: 12px;
  }
  .compatibility-item {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }
  .comparison-table-wrap { overflow-x: auto; }
  .comparison-table {
    width: 100%;
    min-width: 720px;
    border-collapse: collapse;
  }
  .comparison-table th,
  .comparison-table td {
    padding: 10px 12px;
    border-bottom: 1px solid var(--td-component-stroke);
    text-align: left;
  }
  .comparison-table th {
    color: var(--td-text-color-secondary);
    background: var(--td-bg-color-secondarycontainer);
  }
  .comparison-table th small,
  .comparison-table td small {
    display: block;
    margin-top: 3px;
    color: var(--td-text-color-placeholder);
    font-weight: 400;
  }
}
</style>
