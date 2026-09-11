<template>
  <div class="model-usage-panel">
    <div class="usage-header">
      <div>
        <h2>模型调用统计</h2>
        <p>查看当前工作区的模型调用、Token、缓存和耗时记录。</p>
      </div>
      <t-button variant="outline" @click="emit('close')">返回模型配置</t-button>
    </div>

    <div class="usage-filters">
      <label>开始时间<input v-model="filters.startedFrom" type="datetime-local" /></label>
      <label>结束时间<input v-model="filters.startedTo" type="datetime-local" /></label>
      <label>模型
        <select v-model="filters.modelId">
          <option value="">全部模型</option>
          <option v-for="model in models" :key="model.id" :value="model.id">{{ model.name }}</option>
        </select>
      </label>
      <label>Provider<input v-model="filters.provider" type="text" placeholder="全部 Provider" /></label>
      <label>模型类型
        <select v-model="filters.modelType">
          <option value="">全部类型</option>
          <option value="KnowledgeQA">Chat</option>
          <option value="Embedding">Embedding</option>
          <option value="Rerank">Rerank</option>
          <option value="VLLM">VLM</option>
          <option value="ASR">ASR</option>
        </select>
      </label>
      <label>调用操作
        <select v-model="filters.operation">
          <option value="">全部操作</option>
          <option value="chat">Chat</option>
          <option value="chat_stream">Chat Stream</option>
          <option value="embed">Embed</option>
          <option value="batch_embed">Batch Embed</option>
          <option value="rerank">Rerank</option>
          <option value="vlm_predict">VLM</option>
          <option value="asr_transcribe">ASR</option>
        </select>
      </label>
      <label>调用来源
        <select v-model="filters.source">
          <option value="">全部来源</option>
          <option value="chat">Chat</option>
          <option value="evaluation">评测</option>
          <option value="wiki">Wiki</option>
          <option value="ingestion">文档解析</option>
        </select>
      </label>
      <label>状态
        <select v-model="filters.success">
          <option value="">全部状态</option>
          <option value="true">成功</option>
          <option value="false">失败</option>
        </select>
      </label>
      <div class="usage-filter-actions">
        <t-button theme="primary" :loading="loading" @click="reload">查询</t-button>
        <t-button variant="outline" :disabled="loading" @click="resetFilters">重置</t-button>
      </div>
    </div>

    <div v-if="errorMessage" class="usage-error">{{ errorMessage }}</div>

    <div class="usage-cards">
      <div class="usage-card"><span>总调用次数</span><strong>{{ summary.total_calls }}</strong></div>
      <div class="usage-card"><span>成功 / 失败</span><strong>{{ summary.succeeded_calls }} / {{ summary.failed_calls }}</strong></div>
      <div class="usage-card"><span>总 Tokens</span><strong>{{ formatAggregateTokens(summary.total_tokens, summary.tokens_reported_calls) }}</strong></div>
      <div class="usage-card"><span>平均耗时</span><strong>{{ formatDuration(summary.average_duration_ms) }}</strong></div>
      <div class="usage-card"><span>缓存命中率</span><strong>{{ formatRate(summary.cache_hit_rate) }}</strong></div>
      <div class="usage-card"><span>费用</span><strong>{{ formatCost(summary.cost_amount, summary.cost_currency) }}</strong><small v-if="summary.cost_status === 'partial'">费用数据不完整</small></div>
    </div>

    <section class="usage-section">
      <h3>按模型汇总</h3>
      <div class="table-scroll">
        <table class="usage-table">
          <thead><tr><th>模型</th><th>类型 / Provider</th><th>调用</th><th>成功 / 失败</th><th>Tokens</th><th>缓存命中率</th><th>费用</th><th>平均耗时</th></tr></thead>
          <tbody>
            <tr v-for="item in summary.by_model" :key="`${item.model_id}-${item.model_name}-${item.model_type}-${item.provider}`">
              <td>{{ item.model_name }}<small>{{ item.model_id }}</small></td>
              <td>{{ modelTypeLabel(item.model_type) }}<small>{{ item.provider || '—' }}</small></td>
              <td>{{ item.calls }}</td>
              <td>{{ item.succeeded_calls }} / {{ item.failed_calls }}</td>
              <td>{{ formatAggregateTokens(item.total_tokens, item.tokens_reported_calls) }}</td>
              <td>{{ formatRate(item.cache_hit_rate) }}</td>
              <td>{{ formatCost(item.cost_amount, item.cost_currency) }}<small v-if="item.cost_status === 'partial'">费用数据不完整</small></td>
              <td>{{ formatDuration(item.average_duration_ms) }}</td>
            </tr>
            <tr v-if="!loading && summary.by_model.length === 0"><td colspan="8" class="empty-cell">暂无调用汇总</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="usage-section">
      <h3>调用明细</h3>
      <div class="table-scroll">
        <table class="usage-table usage-events-table">
          <thead><tr><th>时间</th><th>模型</th><th>调用类型</th><th>来源</th><th>输入 / 输出 / 总 Tokens</th><th>缓存读取 / 创建 / 未命中</th><th>费用</th><th>耗时</th><th>状态</th></tr></thead>
          <tbody>
            <tr v-for="item in events.items" :key="item.call_id">
              <td>{{ formatDate(item.started_at) }}</td>
              <td>{{ item.model_name }}<small>{{ modelTypeLabel(item.model_type) }} · {{ item.provider || '—' }}</small></td>
              <td>{{ item.operation }}</td>
              <td>{{ item.source || '—' }}</td>
              <td>{{ tokenValue(item.prompt_tokens) }} / {{ tokenValue(item.completion_tokens) }} / {{ tokenValue(item.total_tokens) }}</td>
              <td>{{ tokenValue(item.cache_read_tokens) }} / {{ tokenValue(item.cache_write_tokens) }} / {{ tokenValue(item.cache_miss_tokens) }}</td>
              <td>{{ formatCost(item.cost_amount, item.cost_currency) }}<small v-if="item.cost_source">{{ item.cost_source }}</small></td>
              <td>{{ formatDuration(item.duration_ms) }}</td>
              <td><span :class="['status-chip', item.success ? 'status-chip--success' : 'status-chip--failed']">{{ item.success ? '成功' : '失败' }}</span></td>
            </tr>
            <tr v-if="!loading && events.items.length === 0"><td colspan="9" class="empty-cell">暂无调用明细</td></tr>
          </tbody>
        </table>
      </div>
      <div class="usage-pagination">
        <span>共 {{ events.total }} 条</span>
        <t-button variant="outline" size="small" :disabled="loading || page <= 1" @click="changePage(page - 1)">上一页</t-button>
        <span>第 {{ page }} 页</span>
        <t-button variant="outline" size="small" :disabled="loading || page >= totalPages" @click="changePage(page + 1)">下一页</t-button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  getModelUsageSummary,
  listModelUsageEvents,
  listModels,
  type ModelConfig,
  type ModelUsageEventPage,
  type ModelUsageQuery,
  type ModelUsageSummary,
} from '@/api/model'

const emit = defineEmits<{ close: [] }>()
const pageSize = 20
const loading = ref(false)
const errorMessage = ref('')
const page = ref(1)
const models = ref<ModelConfig[]>([])
const filters = reactive({
  startedFrom: '', startedTo: '', modelId: '', provider: '', modelType: '', operation: '', source: '', success: '',
})
const summary = ref<ModelUsageSummary>({
  total_calls: 0, succeeded_calls: 0, failed_calls: 0, prompt_tokens: 0, completion_tokens: 0,
  total_tokens: 0, cached_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0, cache_miss_tokens: 0,
  cache_reported_calls: 0, cache_hit_calls: 0, cache_miss_calls: 0, tokens_reported_calls: 0, cost_status: 'unavailable', by_model: [],
})
const events = ref<ModelUsageEventPage>({ items: [], page: 1, page_size: pageSize, total: 0 })
const totalPages = computed(() => Math.max(1, Math.ceil(events.value.total / pageSize)))

function toQuery(): ModelUsageQuery {
  const query: ModelUsageQuery = { page: page.value, page_size: pageSize }
  if (filters.startedFrom) query.started_from = new Date(filters.startedFrom).toISOString()
  if (filters.startedTo) query.started_to = new Date(filters.startedTo).toISOString()
  if (filters.modelId) query.model_id = filters.modelId
  if (filters.provider) query.provider = filters.provider
  if (filters.modelType) query.model_type = filters.modelType
  if (filters.operation) query.operation = filters.operation
  if (filters.source) query.source = filters.source
  if (filters.success !== '') query.success = filters.success === 'true'
  return query
}

async function reload() {
  page.value = 1
  await load()
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    const query = toQuery()
    const [nextSummary, nextEvents] = await Promise.all([getModelUsageSummary(query), listModelUsageEvents(query)])
    summary.value = nextSummary
    events.value = nextEvents
  } catch (error: any) {
    errorMessage.value = error?.message || '模型调用统计加载失败'
  } finally {
    loading.value = false
  }
}

async function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value) return
  page.value = nextPage
  await load()
}

function resetFilters() {
  filters.startedFrom = ''
  filters.startedTo = ''
  filters.modelId = ''
  filters.provider = ''
  filters.modelType = ''
  filters.operation = ''
  filters.source = ''
  filters.success = ''
  void reload()
}

function formatNumber(value?: number) { return value == null ? '—' : new Intl.NumberFormat().format(value) }
function formatAggregateTokens(value: number | undefined, reportedCalls: number | undefined) {
  return !reportedCalls ? '—' : formatNumber(value)
}
function tokenValue(value?: number) { return value == null ? '—' : String(value) }
function formatDuration(value?: number) { return value == null ? '—' : `${Math.round(value)} ms` }
function formatRate(value?: number) { return value == null ? '—' : `${(value * 100).toFixed(1)}%` }
function formatCost(value?: number, currency?: string) {
  return value == null || !currency ? '—' : `${currency} ${value.toFixed(6)}`
}
function formatDate(value?: string) { return value ? new Date(value).toLocaleString() : '—' }
function modelTypeLabel(value: string) { return value === 'KnowledgeQA' ? 'Chat' : value || '—' }

onMounted(async () => {
  try { models.value = await listModels() } catch { models.value = [] }
  await load()
})
</script>

<style lang="less" scoped>
.model-usage-panel { width: 100%; color: var(--td-text-color-primary); }
.usage-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
.usage-header h2 { margin: 0 0 8px; font-size: 20px; }
.usage-header p { margin: 0; color: var(--td-text-color-secondary); font-size: 14px; }
.usage-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 12px; padding: 14px; background: var(--td-bg-color-secondarycontainer); border: 1px solid var(--td-component-stroke); border-radius: 8px; }
.usage-filters label { display: flex; flex-direction: column; gap: 5px; color: var(--td-text-color-secondary); font-size: 12px; }
.usage-filters input, .usage-filters select { min-width: 150px; height: 32px; padding: 0 8px; color: var(--td-text-color-primary); background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: 4px; }
.usage-filter-actions { display: flex; gap: 8px; }
.usage-error { margin-top: 12px; padding: 10px 12px; color: var(--td-error-color); background: var(--td-error-color-1); border-radius: 6px; }
.usage-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin: 18px 0; }
.usage-card { padding: 14px 16px; background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: 8px; }
.usage-card span { display: block; color: var(--td-text-color-secondary); font-size: 12px; }
.usage-card strong { display: block; margin-top: 8px; font-size: 20px; font-weight: 600; }
.usage-card small { display: block; margin-top: 3px; color: var(--td-text-color-placeholder); font-size: 11px; }
.usage-section { margin-top: 22px; }
.usage-section h3 { margin: 0 0 10px; font-size: 16px; }
.table-scroll { overflow-x: auto; border: 1px solid var(--td-component-stroke); border-radius: 8px; }
.usage-table { width: 100%; min-width: 900px; border-collapse: collapse; font-size: 12px; }
.usage-table th, .usage-table td { padding: 10px 12px; text-align: left; white-space: nowrap; border-bottom: 1px solid var(--td-component-stroke); }
.usage-table th { color: var(--td-text-color-secondary); font-weight: 500; background: var(--td-bg-color-secondarycontainer); }
.usage-table tr:last-child td { border-bottom: 0; }
.usage-table small { display: block; margin-top: 3px; color: var(--td-text-color-placeholder); font-size: 11px; }
.empty-cell { padding: 28px !important; color: var(--td-text-color-placeholder); text-align: center !important; }
.status-chip { display: inline-flex; padding: 2px 7px; border-radius: 10px; font-size: 11px; }
.status-chip--success { color: var(--td-success-color); background: var(--td-success-color-1); }
.status-chip--failed { color: var(--td-error-color); background: var(--td-error-color-1); }
.usage-pagination { display: flex; align-items: center; justify-content: flex-end; gap: 12px; padding-top: 12px; color: var(--td-text-color-secondary); }
@media (max-width: 760px) { .usage-header { flex-direction: column; } .usage-filters label { width: 100%; } .usage-filters input, .usage-filters select { width: 100%; } .usage-pagination { justify-content: center; } }
</style>
