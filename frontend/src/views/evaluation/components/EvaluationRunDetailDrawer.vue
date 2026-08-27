<template>
  <t-drawer
    :visible="visible"
    :header="t('evaluation.detail.title')"
    size="82%"
    :footer="false"
    destroy-on-close
    @update:visible="emit('update:visible', $event)"
  >
    <t-loading :loading="loading">
      <div v-if="overview" class="evaluation-detail">
        <section class="detail-section">
          <div class="section-heading">
            <h3>{{ t('evaluation.detail.summary') }}</h3>
            <t-tag :theme="statusTheme(overview.summary.status)" variant="light">
              {{ statusLabel(overview.summary.status) }}
            </t-tag>
          </div>
          <div class="summary-grid">
            <div class="summary-item">
              <span>{{ t('evaluation.fields.runId') }}</span>
              <code>{{ overview.summary.run_id }}</code>
            </div>
            <div class="summary-item">
              <span>{{ t('evaluation.fields.dataset') }}</span>
              <strong>{{ overview.summary.dataset.id }}</strong>
            </div>
            <div class="summary-item">
              <span>{{ t('evaluation.fields.configHash') }}</span>
              <code :title="overview.summary.config_hash">{{ shortID(overview.summary.config_hash) }}</code>
            </div>
            <div class="summary-item">
              <span>{{ t('evaluation.fields.startedAt') }}</span>
              <strong>{{ formatDate(overview.summary.started_at) }}</strong>
            </div>
            <div class="summary-item">
              <span>{{ t('evaluation.fields.completedAt') }}</span>
              <strong>{{ formatDate(overview.summary.completed_at) }}</strong>
            </div>
            <div class="summary-item">
              <span>{{ t('evaluation.fields.reproducibility') }}</span>
              <strong>{{ overview.summary.reproducibility.status || '—' }}</strong>
            </div>
          </div>
          <p v-if="overview.summary.error_message" class="error-message">
            {{ overview.summary.error_message }}
          </p>
        </section>

        <section class="detail-section">
          <h3>{{ t('evaluation.detail.dimensions') }}</h3>
          <div class="dimension-grid">
            <article class="dimension-card">
              <span>{{ t('evaluation.dimensions.retrieval') }}</span>
              <strong>{{ formatEvaluationNumber(overview.summary.retrieval?.precision) }}</strong>
              <small>
                {{ t('evaluation.metrics.precision') }} ·
                {{ t('evaluation.metrics.recall') }} {{ formatEvaluationNumber(overview.summary.retrieval?.recall) }} ·
                MRR {{ formatEvaluationNumber(overview.summary.retrieval?.mrr) }}
              </small>
            </article>
            <article class="dimension-card">
              <span>{{ t('evaluation.dimensions.answer') }}</span>
              <strong>{{ formatEvaluationNumber(overview.summary.answer?.rougel) }}</strong>
              <small>
                ROUGE-L · BLEU-1 {{ formatEvaluationNumber(overview.summary.answer?.bleu1) }}
              </small>
            </article>
            <article class="dimension-card">
              <span>{{ t('evaluation.dimensions.cost') }}</span>
              <strong>{{ costText(overview.summary.cost) }}</strong>
              <small>
                {{ overview.summary.cost.status }} ·
                {{ t('evaluation.fields.calls') }} {{ overview.summary.usage.calls.total || 0 }}
              </small>
            </article>
            <article class="dimension-card">
              <span>{{ t('evaluation.dimensions.timing') }}</span>
              <strong>{{ formatEvaluationDuration(overview.summary.timing.total_wall_time_ms) }}</strong>
              <small>
                P95 {{ formatEvaluationDuration(overview.summary.timing.case_p95_ms) }}
              </small>
            </article>
          </div>
        </section>

        <section class="detail-section">
          <div class="section-heading">
            <h3>{{ t('evaluation.detail.configuration') }}</h3>
            <span class="version-text">
              {{ overview.summary.metric_version }} · {{ overview.summary.result_version }}
            </span>
          </div>
          <div class="model-grid">
            <div>
              <span>{{ t('evaluation.models.embedding') }}</span>
              <strong>{{ modelLabel(overview.summary.models.embedding) }}</strong>
            </div>
            <div>
              <span>{{ t('evaluation.models.chat') }}</span>
              <strong>{{ modelLabel(overview.summary.models.chat) }}</strong>
            </div>
            <div>
              <span>{{ t('evaluation.models.rerank') }}</span>
              <strong>{{ modelLabel(overview.summary.models.rerank) }}</strong>
            </div>
          </div>
          <details v-if="overview.config" class="config-json">
            <summary>{{ t('evaluation.detail.rawConfiguration') }}</summary>
            <pre>{{ JSON.stringify(overview.config, null, 2) }}</pre>
          </details>
        </section>

        <section class="detail-section">
          <div class="section-heading case-heading">
            <div>
              <h3>{{ t('evaluation.detail.cases') }}</h3>
              <p>{{ t('evaluation.detail.casesHint') }}</p>
            </div>
            <t-select
              v-model="caseStatus"
              :placeholder="t('evaluation.filters.allStatuses')"
              clearable
              class="case-status-select"
              :options="statusOptions"
              @change="resetCasePage"
            />
          </div>
          <t-table
            row-key="case_id"
            :data="cases.items"
            :columns="caseColumns"
            :loading="casesLoading"
            size="medium"
            hover
          >
            <template #status="{ row }">
              <t-tag :theme="statusTheme(row.status)" variant="light">
                {{ statusLabel(row.status) }}
              </t-tag>
            </template>
            <template #question="{ row }">
              <div class="fingerprint-cell">
                <strong>#{{ row.evidence.qid }}</strong>
                <code :title="row.evidence.question_fingerprint">
                  {{ shortID(row.evidence.question_fingerprint) }}
                </code>
              </div>
            </template>
            <template #retrieval="{ row }">
              <span>{{ formatEvaluationNumber(row.evidence.metrics?.retrieval_metrics.precision) }}</span>
            </template>
            <template #answer="{ row }">
              <span>{{ formatEvaluationNumber(row.evidence.metrics?.generation_metrics.rougel) }}</span>
            </template>
            <template #duration="{ row }">
              <span>{{ formatEvaluationDuration(row.duration_ms) }}</span>
            </template>
            <template #evidence="{ row }">
              <span>
                GT {{ row.evidence.ground_truth_pids?.length || 0 }} /
                {{ t('evaluation.detail.retrieved') }} {{ row.evidence.metric_input_pids?.length || 0 }}
              </span>
            </template>
          </t-table>
          <t-pagination
            v-if="cases.total > cases.page_size"
            class="case-pagination"
            :total="cases.total"
            :current="cases.page"
            :page-size="cases.page_size"
            :show-jumper="false"
            :show-page-size="false"
            @current-change="changeCasePage"
          />
        </section>
      </div>
      <t-empty v-else-if="!loading" :description="t('evaluation.detail.unavailable')" />
    </t-loading>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getEvaluationRun,
  listEvaluationRunCases,
  type EvaluationCasePage,
  type EvaluationCostResult,
  type EvaluationModelConfig,
  type EvaluationRunOverview,
  type EvaluationRunStatus,
} from '@/api/evaluation'
import {
  formatEvaluationCost,
  formatEvaluationDuration,
  formatEvaluationNumber,
} from '../evaluationComparison'

const props = defineProps<{
  visible: boolean
  runId: string
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
}>()

const { t } = useI18n()
const loading = ref(false)
const casesLoading = ref(false)
const overview = ref<EvaluationRunOverview | null>(null)
const caseStatus = ref<EvaluationRunStatus | undefined>()
const cases = reactive<EvaluationCasePage>({ items: [], total: 0, page: 1, page_size: 20 })

const statusValues: EvaluationRunStatus[] = ['pending', 'running', 'success', 'partial', 'failed']
const statusOptions = computed(() => statusValues.map((value) => ({
  value,
  label: statusLabel(value),
})))

const caseColumns = computed(() => [
  { colKey: 'case_id', title: t('evaluation.fields.caseId'), width: 130 },
  { colKey: 'status', title: t('evaluation.fields.status'), width: 110 },
  { colKey: 'question', title: t('evaluation.detail.questionEvidence'), minWidth: 210 },
  { colKey: 'retrieval', title: t('evaluation.metrics.precision'), width: 110 },
  { colKey: 'answer', title: 'ROUGE-L', width: 110 },
  { colKey: 'duration', title: t('evaluation.fields.duration'), width: 110 },
  { colKey: 'evidence', title: t('evaluation.detail.evidence'), minWidth: 150 },
])

watch(
  () => [props.visible, props.runId] as const,
  ([visible, runID]) => {
    if (visible && runID) void loadDetail(runID)
  },
  { immediate: true },
)

async function loadDetail(runID: string) {
  loading.value = true
  overview.value = null
  caseStatus.value = undefined
  cases.page = 1
  try {
    const [overviewResponse] = await Promise.all([
      getEvaluationRun(runID),
      loadCases(runID),
    ])
    overview.value = overviewResponse.data
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('evaluation.messages.loadDetailFailed'))
  } finally {
    loading.value = false
  }
}

async function loadCases(runID = props.runId) {
  if (!runID) return
  casesLoading.value = true
  try {
    const response = await listEvaluationRunCases(runID, {
      status: caseStatus.value,
      page: cases.page,
      page_size: cases.page_size,
    })
    Object.assign(cases, response.data)
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('evaluation.messages.loadCasesFailed'))
  } finally {
    casesLoading.value = false
  }
}

function resetCasePage() {
  cases.page = 1
  void loadCases()
}

function changeCasePage(page: number) {
  cases.page = page
  void loadCases()
}

function statusLabel(status: EvaluationRunStatus) {
  return t(`evaluation.status.${status}`)
}

function statusTheme(status: EvaluationRunStatus) {
  if (status === 'success') return 'success'
  if (status === 'partial') return 'warning'
  if (status === 'failed') return 'danger'
  if (status === 'running') return 'primary'
  return 'default'
}

function formatDate(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function shortID(value?: string) {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 18)}…` : value
}

function modelLabel(model?: EvaluationModelConfig | null) {
  if (!model) return t('evaluation.common.notConfigured')
  return model.display_name || model.name || model.id || '—'
}

function costText(cost: EvaluationCostResult) {
  return formatEvaluationCost(cost.amount, cost.currency) || t('evaluation.common.unavailable')
}
</script>

<style scoped lang="less">
.evaluation-detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.detail-section {
  padding: 18px;
  border: 1px solid var(--td-component-border);
  border-radius: 10px;
  background: var(--td-bg-color-container);

  h3 {
    margin: 0 0 14px;
    font-size: 16px;
  }
}

.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;

  h3 { margin-bottom: 14px; }
}

.summary-grid,
.model-grid,
.dimension-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 12px;
}

.summary-item,
.model-grid > div {
  display: flex;
  flex-direction: column;
  gap: 6px;

  span {
    color: var(--td-text-color-placeholder);
    font-size: 12px;
  }

  code, strong {
    overflow: hidden;
    color: var(--td-text-color-primary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.dimension-card {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 14px;
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);

  span, small { color: var(--td-text-color-secondary); }
  strong { font-size: 22px; }
}

.error-message {
  margin: 14px 0 0;
  padding: 10px;
  border-radius: 6px;
  color: var(--td-error-color);
  background: var(--td-error-color-light);
}

.version-text,
.case-heading p {
  margin: 0;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
}

.config-json {
  margin-top: 16px;

  summary { cursor: pointer; color: var(--td-brand-color); }
  pre {
    max-height: 360px;
    overflow: auto;
    padding: 14px;
    border-radius: 6px;
    background: var(--td-bg-color-secondarycontainer);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
  }
}

.case-heading { align-items: flex-start; }
.case-status-select { width: 170px; }
.case-pagination { margin-top: 16px; justify-content: flex-end; }

.fingerprint-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;

  code { color: var(--td-text-color-placeholder); font-size: 11px; }
}
</style>
