<template>
  <div class="evaluation-page">
    <header class="page-header">
      <div>
        <h1>{{ t('evaluation.title') }}</h1>
        <p>{{ t('evaluation.description') }}</p>
      </div>
      <div class="header-actions">
        <t-button variant="outline" :loading="loading" @click="loadRuns">
          <template #icon><t-icon name="refresh" /></template>
          {{ t('evaluation.actions.refresh') }}
        </t-button>
        <t-button
          variant="outline"
          :disabled="selectedRuns.length < 2 || selectedRuns.length > 5"
          @click="comparisonVisible = true"
        >
          <template #icon><t-icon name="chart-combo" /></template>
          {{ t('evaluation.actions.compare') }}
        </t-button>
        <t-button v-if="canStartEvaluation" theme="primary" @click="openStartDialog">
          <template #icon><t-icon name="add" /></template>
          {{ t('evaluation.actions.start') }}
        </t-button>
      </div>
    </header>

    <section v-if="activeTask" class="active-run" role="status">
      <div>
        <span class="live-dot" />
        <strong>{{ t('evaluation.running.title') }}</strong>
        <code>{{ activeTask.id }}</code>
      </div>
      <span>
        {{ activeTask.finished || 0 }} / {{ activeTask.total || 0 }}
      </span>
    </section>

    <section class="filter-panel">
      <div class="filter-grid">
        <label>
          <span>{{ t('evaluation.fields.status') }}</span>
          <t-select
            v-model="filters.status"
            clearable
            :placeholder="t('evaluation.filters.allStatuses')"
            :options="statusOptions"
          />
        </label>
        <label>
          <span>{{ t('evaluation.fields.dataset') }}</span>
          <t-input v-model="filters.dataset_id" clearable :placeholder="t('evaluation.filters.datasetPlaceholder')" />
        </label>
        <label>
          <span>{{ t('evaluation.fields.configHash') }}</span>
          <t-input v-model="filters.config_hash" clearable :placeholder="t('evaluation.filters.configPlaceholder')" />
        </label>
        <label>
          <span>{{ t('evaluation.models.embedding') }}</span>
          <t-select
            v-model="filters.embedding_model_id"
            clearable
            filterable
            :options="embeddingOptions"
            :placeholder="t('evaluation.filters.modelPlaceholder')"
          />
        </label>
        <label>
          <span>{{ t('evaluation.models.chat') }}</span>
          <t-select
            v-model="filters.chat_model_id"
            clearable
            filterable
            :options="chatOptions"
            :placeholder="t('evaluation.filters.modelPlaceholder')"
          />
        </label>
        <label>
          <span>{{ t('evaluation.models.rerank') }}</span>
          <t-select
            v-model="filters.rerank_model_id"
            clearable
            filterable
            :options="rerankOptions"
            :placeholder="t('evaluation.filters.modelPlaceholder')"
          />
        </label>
        <label>
          <span>{{ t('evaluation.filters.startedFrom') }}</span>
          <input v-model="filters.started_from_local" class="native-datetime" type="datetime-local" />
        </label>
        <label>
          <span>{{ t('evaluation.filters.startedTo') }}</span>
          <input v-model="filters.started_to_local" class="native-datetime" type="datetime-local" />
        </label>
      </div>
      <div class="filter-actions">
        <t-button variant="outline" @click="resetFilters">{{ t('evaluation.actions.reset') }}</t-button>
        <t-button theme="primary" @click="applyFilters">{{ t('evaluation.actions.search') }}</t-button>
      </div>
    </section>

    <section class="history-panel">
      <div class="history-heading">
        <div>
          <h2>{{ t('evaluation.history.title') }}</h2>
          <span>{{ t('evaluation.history.total', { count: pageData.total }) }}</span>
        </div>
        <div v-if="selectedRuns.length" class="selection-summary">
          <span>{{ t('evaluation.comparison.selected', { count: selectedRuns.length }) }}</span>
          <t-button variant="text" size="small" @click="selectedRunIDs = []">
            {{ t('evaluation.actions.clear') }}
          </t-button>
        </div>
      </div>

      <t-table
        row-key="run_id"
        :data="pageData.items"
        :columns="columns"
        :loading="loading"
        size="medium"
        hover
        class="history-table"
      >
        <template #select="{ row }">
          <t-checkbox
            :checked="selectedRunIDs.includes(row.run_id)"
            :disabled="!selectedRunIDs.includes(row.run_id) && selectedRunIDs.length >= 5"
            @change="toggleRunSelection(row)"
          />
        </template>
        <template #status="{ row }">
          <t-tag :theme="statusTheme(row.status)" variant="light">
            {{ statusLabel(row.status) }}
          </t-tag>
        </template>
        <template #run="{ row }">
          <div class="primary-cell">
            <button type="button" class="run-link" @click="openDetail(row.run_id)">
              {{ shortID(row.run_id) }}
            </button>
            <span>{{ row.dataset.id }}</span>
          </div>
        </template>
        <template #config="{ row }">
          <div class="primary-cell">
            <code :title="row.config_hash">{{ shortID(row.config_hash) }}</code>
            <span>{{ row.metric_version }} · {{ row.result_version }}</span>
          </div>
        </template>
        <template #models="{ row }">
          <div class="model-cell">
            <span>{{ t('evaluation.models.embeddingShort') }} {{ modelLabel(row.models.embedding) }}</span>
            <span>{{ t('evaluation.models.chatShort') }} {{ modelLabel(row.models.chat) }}</span>
            <span>{{ t('evaluation.models.rerankShort') }} {{ modelLabel(row.models.rerank) }}</span>
          </div>
        </template>
        <template #retrieval="{ row }">
          <div class="metric-cell">
            <strong>{{ formatEvaluationNumber(row.retrieval?.precision) }}</strong>
            <span>P / R {{ formatEvaluationNumber(row.retrieval?.recall) }}</span>
            <span>MRR {{ formatEvaluationNumber(row.retrieval?.mrr) }}</span>
          </div>
        </template>
        <template #answer="{ row }">
          <div class="metric-cell">
            <strong>{{ formatEvaluationNumber(row.answer?.rougel) }}</strong>
            <span>ROUGE-L</span>
            <span>BLEU-1 {{ formatEvaluationNumber(row.answer?.bleu1) }}</span>
          </div>
        </template>
        <template #cost="{ row }">
          <div class="metric-cell">
            <strong>{{ costText(row.cost) }}</strong>
            <span>{{ row.cost.status || t('evaluation.common.unavailable') }}</span>
            <span>{{ t('evaluation.fields.calls') }} {{ row.usage.calls.total || 0 }}</span>
          </div>
        </template>
        <template #timing="{ row }">
          <div class="metric-cell">
            <strong>{{ formatEvaluationDuration(row.timing.total_wall_time_ms) }}</strong>
            <span>P95 {{ formatEvaluationDuration(row.timing.case_p95_ms) }}</span>
          </div>
        </template>
        <template #progress="{ row }">
          <div class="progress-cell">
            <span>{{ row.progress.finished }} / {{ row.progress.total }}</span>
            <t-progress
              :percentage="progressPercent(row.progress.finished, row.progress.total)"
              size="small"
              :label="false"
            />
          </div>
        </template>
        <template #started_at="{ row }">
          <span>{{ formatDate(row.started_at) }}</span>
        </template>
        <template #actions="{ row }">
          <t-button variant="text" theme="primary" size="small" @click="openDetail(row.run_id)">
            {{ t('evaluation.actions.details') }}
          </t-button>
        </template>
      </t-table>

      <t-pagination
        v-if="pageData.total > 0"
        v-model="pageData.page"
        v-model:page-size="pageData.page_size"
        class="history-pagination"
        :total="pageData.total"
        show-jumper
        show-page-number
        show-page-size
        :page-size-options="[10, 20, 50]"
        @change="loadRuns"
      />
    </section>

    <t-dialog
      v-model:visible="startDialogVisible"
      :header="t('evaluation.start.title')"
      width="560px"
      :confirm-btn="{ content: t('evaluation.actions.start'), loading: starting }"
      :cancel-btn="t('common.cancel')"
      :close-on-overlay-click="false"
      @confirm="submitEvaluation"
    >
      <div class="start-form">
        <label>
          <span>{{ t('evaluation.start.dataset') }}</span>
          <t-input v-model="startForm.dataset_id" :placeholder="t('evaluation.start.datasetPlaceholder')" />
          <small>{{ t('evaluation.start.datasetHint') }}</small>
        </label>
        <label>
          <span>{{ t('evaluation.start.knowledgeBase') }}</span>
          <t-select
            v-model="startForm.knowledge_base_id"
            filterable
            :options="knowledgeBaseOptions"
            :placeholder="t('evaluation.start.knowledgeBasePlaceholder')"
          />
          <small>{{ t('evaluation.start.embeddingHint') }}</small>
        </label>
        <label>
          <span>{{ t('evaluation.models.chat') }}</span>
          <t-select
            v-model="startForm.chat_id"
            filterable
            :options="chatOptions"
            :placeholder="t('evaluation.start.chatPlaceholder')"
          />
        </label>
        <label>
          <span>{{ t('evaluation.models.rerank') }}</span>
          <t-select
            v-model="startForm.rerank_id"
            clearable
            filterable
            :options="rerankOptions"
            :placeholder="t('evaluation.start.rerankPlaceholder')"
          />
        </label>
      </div>
    </t-dialog>

    <EvaluationRunDetailDrawer
      v-model:visible="detailVisible"
      :run-id="detailRunID"
    />
    <EvaluationComparisonDrawer
      v-model:visible="comparisonVisible"
      :runs="selectedRuns"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { listModels, type ModelConfig } from '@/api/model'
import {
  getEvaluationTask,
  listEvaluationRuns,
  startEvaluation,
  type EvaluationCostResult,
  type EvaluationModelConfig,
  type EvaluationRunListParams,
  type EvaluationRunPage,
  type EvaluationRunStatus,
  type EvaluationRunSummary,
  type EvaluationTask,
} from '@/api/evaluation'
import EvaluationRunDetailDrawer from './components/EvaluationRunDetailDrawer.vue'
import EvaluationComparisonDrawer from './components/EvaluationComparisonDrawer.vue'
import {
  formatEvaluationCost,
  formatEvaluationDuration,
  formatEvaluationNumber,
  isTerminalEvaluationTaskStatus,
  localDateTimeToRFC3339,
} from './evaluationComparison'

interface KnowledgeBaseOptionSource {
  id: string
  name?: string
}

const { t } = useI18n()
const authStore = useAuthStore()
const loading = ref(false)
const resourcesLoading = ref(false)
const starting = ref(false)
const startDialogVisible = ref(false)
const detailVisible = ref(false)
const comparisonVisible = ref(false)
const detailRunID = ref('')
const selectedRunIDs = ref<string[]>([])
const activeTask = ref<EvaluationTask | null>(null)
const models = ref<ModelConfig[]>([])
const knowledgeBases = ref<KnowledgeBaseOptionSource[]>([])
const pageData = reactive<EvaluationRunPage>({ items: [], total: 0, page: 1, page_size: 20 })
let pollTimer: number | undefined

const filters = reactive({
  status: undefined as EvaluationRunStatus | undefined,
  dataset_id: '',
  config_hash: '',
  embedding_model_id: '',
  chat_model_id: '',
  rerank_model_id: '',
  started_from_local: '',
  started_to_local: '',
})

const startForm = reactive({
  dataset_id: 'default',
  knowledge_base_id: '',
  chat_id: '',
  rerank_id: '',
})

const statusValues: EvaluationRunStatus[] = ['pending', 'running', 'success', 'partial', 'failed']
const statusOptions = computed(() => statusValues.map((value) => ({ value, label: statusLabel(value) })))
const canStartEvaluation = computed(() => authStore.hasRole('admin'))
const selectedRuns = computed(() => selectedRunIDs.value
  .map((runID) => pageData.items.find((run) => run.run_id === runID))
  .filter((run): run is EvaluationRunSummary => Boolean(run)))

const modelOptions = (type: ModelConfig['type']) => computed(() => models.value
  .filter((model) => model.type === type && model.id)
  .map((model) => ({ value: model.id as string, label: model.display_name || model.name || model.id })))
const embeddingOptions = modelOptions('Embedding')
const chatOptions = modelOptions('KnowledgeQA')
const rerankOptions = modelOptions('Rerank')
const knowledgeBaseOptions = computed(() => knowledgeBases.value.map((kb) => ({
  value: kb.id,
  label: kb.name || kb.id,
})))

const columns = computed(() => [
  { colKey: 'select', title: '', width: 48, fixed: 'left' },
  { colKey: 'status', title: t('evaluation.fields.status'), width: 105, fixed: 'left' },
  { colKey: 'run', title: t('evaluation.fields.run'), minWidth: 170, fixed: 'left' },
  { colKey: 'config', title: t('evaluation.fields.configuration'), minWidth: 190 },
  { colKey: 'models', title: t('evaluation.fields.models'), minWidth: 210 },
  { colKey: 'retrieval', title: t('evaluation.dimensions.retrieval'), width: 135 },
  { colKey: 'answer', title: t('evaluation.dimensions.answer'), width: 135 },
  { colKey: 'cost', title: t('evaluation.dimensions.cost'), width: 140 },
  { colKey: 'timing', title: t('evaluation.dimensions.timing'), width: 130 },
  { colKey: 'progress', title: t('evaluation.fields.progress'), width: 130 },
  { colKey: 'started_at', title: t('evaluation.fields.startedAt'), width: 180 },
  { colKey: 'actions', title: t('evaluation.fields.actions'), width: 90, fixed: 'right' },
])

onMounted(() => {
  void Promise.all([loadRuns(), loadResources()])
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onUnmounted(() => {
  stopPolling()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

async function loadRuns() {
  loading.value = true
  try {
    const params: EvaluationRunListParams = {
      status: filters.status,
      dataset_id: filters.dataset_id || undefined,
      config_hash: filters.config_hash || undefined,
      embedding_model_id: filters.embedding_model_id || undefined,
      chat_model_id: filters.chat_model_id || undefined,
      rerank_model_id: filters.rerank_model_id || undefined,
      started_from: localDateTimeToRFC3339(filters.started_from_local),
      started_to: localDateTimeToRFC3339(filters.started_to_local),
      page: pageData.page,
      page_size: pageData.page_size,
    }
    const response = await listEvaluationRuns(params)
    Object.assign(pageData, response.data)
    const visibleIDs = new Set(pageData.items.map((run) => run.run_id))
    selectedRunIDs.value = selectedRunIDs.value.filter((runID) => visibleIDs.has(runID))
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('evaluation.messages.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadResources() {
  if (resourcesLoading.value) return
  resourcesLoading.value = true
  try {
    const [modelList, kbResponse] = await Promise.all([listModels(), listKnowledgeBases()])
    models.value = modelList
    const rows = (kbResponse as any)?.data
    knowledgeBases.value = Array.isArray(rows) ? rows : []
  } catch (error) {
    console.warn('[evaluation] failed to load start-form resources', error)
  } finally {
    resourcesLoading.value = false
  }
}

function applyFilters() {
  pageData.page = 1
  selectedRunIDs.value = []
  void loadRuns()
}

function resetFilters() {
  Object.assign(filters, {
    status: undefined,
    dataset_id: '',
    config_hash: '',
    embedding_model_id: '',
    chat_model_id: '',
    rerank_model_id: '',
    started_from_local: '',
    started_to_local: '',
  })
  applyFilters()
}

function openStartDialog() {
  startDialogVisible.value = true
  if (!models.value.length || !knowledgeBases.value.length) void loadResources()
}

async function submitEvaluation() {
  if (!startForm.dataset_id.trim() || !startForm.knowledge_base_id || !startForm.chat_id) {
    MessagePlugin.warning(t('evaluation.messages.requiredFields'))
    return
  }
  starting.value = true
  try {
    const response = await startEvaluation({
      dataset_id: startForm.dataset_id.trim(),
      knowledge_base_id: startForm.knowledge_base_id,
      chat_id: startForm.chat_id,
      rerank_id: startForm.rerank_id || undefined,
    })
    activeTask.value = response.data.task
    startDialogVisible.value = false
    MessagePlugin.success(t('evaluation.messages.started'))
    pageData.page = 1
    await loadRuns()
    schedulePolling(1000)
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('evaluation.messages.startFailed'))
  } finally {
    starting.value = false
  }
}

function schedulePolling(delay = 2000) {
  stopPolling(false)
  if (!activeTask.value || isTerminalEvaluationTaskStatus(activeTask.value.status) || document.hidden) return
  pollTimer = window.setTimeout(pollEvaluationTask, delay)
}

async function pollEvaluationTask() {
  const taskID = activeTask.value?.id
  if (!taskID || document.hidden) return
  try {
    const response = await getEvaluationTask(taskID)
    activeTask.value = response.data.task
    if (isTerminalEvaluationTaskStatus(response.data.task.status)) {
      const failed = response.data.task.status === 3
      stopPolling()
      await loadRuns()
      if (failed) {
        MessagePlugin.error(response.data.task.err_msg || t('evaluation.messages.runFailed'))
      } else {
        MessagePlugin.success(t('evaluation.messages.completed'))
        openDetail(taskID)
      }
      return
    }
    schedulePolling()
  } catch (error: any) {
    stopPolling()
    MessagePlugin.error(error?.message || t('evaluation.messages.pollFailed'))
  }
}

function stopPolling(clearTask = true) {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  pollTimer = undefined
  if (clearTask) activeTask.value = null
}

function handleVisibilityChange() {
  if (document.hidden) {
    if (pollTimer !== undefined) window.clearTimeout(pollTimer)
    pollTimer = undefined
    return
  }
  if (activeTask.value && !isTerminalEvaluationTaskStatus(activeTask.value.status)) schedulePolling(250)
}

function toggleRunSelection(run: EvaluationRunSummary) {
  const index = selectedRunIDs.value.indexOf(run.run_id)
  if (index >= 0) {
    selectedRunIDs.value.splice(index, 1)
    return
  }
  if (selectedRunIDs.value.length >= 5) {
    MessagePlugin.warning(t('evaluation.messages.compareLimit'))
    return
  }
  selectedRunIDs.value.push(run.run_id)
}

function openDetail(runID: string) {
  detailRunID.value = runID
  detailVisible.value = true
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

function formatDate(value?: string | null) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function progressPercent(finished: number, total: number) {
  if (!total) return 0
  return Math.min(100, Math.round((finished / total) * 100))
}
</script>

<style scoped lang="less">
.evaluation-page {
  min-height: 100%;
  padding: 28px 32px 48px;
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-page);
}

.page-header,
.history-heading,
.filter-actions,
.active-run {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.page-header {
  margin-bottom: 20px;

  h1 { margin: 0; font-size: 26px; }
  p { margin: 6px 0 0; color: var(--td-text-color-secondary); }
}

.header-actions { display: flex; flex-wrap: wrap; gap: 10px; }

.active-run {
  margin-bottom: 16px;
  padding: 12px 16px;
  border: 1px solid var(--td-brand-color-light);
  border-radius: 8px;
  background: var(--td-brand-color-light);

  > div { display: flex; align-items: center; gap: 9px; }
  code { color: var(--td-text-color-secondary); }
}

.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--td-success-color);
  box-shadow: 0 0 0 4px var(--td-success-color-light);
}

.filter-panel,
.history-panel {
  border: 1px solid var(--td-component-border);
  border-radius: 10px;
  background: var(--td-bg-color-container);
}

.filter-panel { margin-bottom: 18px; padding: 18px; }
.filter-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(170px, 1fr));
  gap: 14px;

  label, .start-form label {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  label > span { color: var(--td-text-color-secondary); font-size: 12px; }
}

.native-datetime {
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--td-border-level-2-color);
  border-radius: 3px;
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-container);
  outline: none;

  &:focus { border-color: var(--td-brand-color); }
}

.filter-actions { justify-content: flex-end; margin-top: 16px; }
.history-panel { overflow: hidden; }
.history-heading { padding: 16px 18px; border-bottom: 1px solid var(--td-component-stroke); }
.history-heading h2 { display: inline; margin: 0 10px 0 0; font-size: 18px; }
.history-heading span { color: var(--td-text-color-secondary); }
.selection-summary { display: flex; align-items: center; gap: 8px; }
.history-table { width: 100%; }
.history-pagination { padding: 16px 18px; justify-content: flex-end; }

.primary-cell,
.model-cell,
.metric-cell,
.progress-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.primary-cell span,
.model-cell span,
.metric-cell span {
  color: var(--td-text-color-placeholder);
  font-size: 11px;
}

.run-link {
  overflow: hidden;
  padding: 0;
  border: 0;
  color: var(--td-brand-color);
  background: transparent;
  font: inherit;
  font-weight: 600;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
}

.metric-cell strong { font-variant-numeric: tabular-nums; }
.progress-cell span { font-size: 12px; }

.start-form {
  display: flex;
  flex-direction: column;
  gap: 16px;

  label { display: flex; flex-direction: column; gap: 6px; }
  label > span { font-weight: 500; }
  small { color: var(--td-text-color-placeholder); }
}

@media (max-width: 1180px) {
  .filter-grid { grid-template-columns: repeat(2, minmax(180px, 1fr)); }
}

@media (max-width: 720px) {
  .evaluation-page { padding: 20px 16px 36px; }
  .page-header { align-items: flex-start; flex-direction: column; }
  .filter-grid { grid-template-columns: 1fr; }
}
</style>
