<template>
  <div>
    <h2 style="margin: 0 0 16px">凭证查询</h2>
    <ReloadBanner :failed="loadFailed" @retry="init" />

    <div class="panel">
      <div class="filters">
        <el-date-picker v-model="month" type="month" value-format="YYYY-MM" style="width: 150px" :clearable="true"
          placeholder="全部月份" @change="load" />
        <el-radio-group v-model="status" @change="load">
          <el-radio-button value="">全部</el-radio-button>
          <el-radio-button value="normal">已记账</el-radio-button>
          <el-radio-button value="voided">已作废</el-radio-button>
        </el-radio-group>
        <el-button size="default" @click="load">刷新</el-button>
        <span class="hint">共 {{ list.length }} 张，借方合计 ¥ {{ fmt(total) }}（点击行查看详情与打印）</span>
      </div>

      <el-table :data="list" size="default" border :show-summary="list.length > 0" :summary-method="sumAmount" @row-click="open" highlight-current-row>
        <el-table-column label="凭证来源" width="110" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="{ manual: 'primary', voucher: 'success', agg: 'success', closing: 'warning', opening: 'info' }[row.sourceType]">
              {{ sourceName(row.sourceType) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="no" label="凭证号" width="180" />
        <el-table-column prop="date" label="凭证日期" width="110" />
        <el-table-column prop="summary" label="摘要" min-width="220" show-overflow-tooltip />
        <el-table-column prop="createdBy" label="制单人" width="90">
          <template #default="{ row }">{{ row.createdBy || '—' }}</template>
        </el-table-column>
        <el-table-column label="金额" align="right" width="150">
          <template #default="{ row }">{{ fmt(row.amount) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'normal' ? 'success' : 'danger'">
              {{ row.status === 'normal' ? '已记账' : '已作废' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-drawer v-model="drawer" title="记账凭证详情" size="520px">
      <div v-if="detail">
        <p style="margin-top:0">
          <b>{{ detail.no }}</b>　{{ detail.date }}
          <el-button size="small" type="primary" style="float: right" @click="printOne">打印记帐凭证</el-button>
          <el-button v-if="detail.status === 'normal' && detail.sourceType === 'manual'" size="small" type="danger"
            style="float: right; margin-right: 8px" @click="voidOne">作废</el-button>
        </p>
        <el-table :data="detail.entries" size="small" border>
          <el-table-column prop="summary" label="摘要" min-width="150" />
          <el-table-column label="会计科目" min-width="170">
            <template #default="{ row }">
              {{ row.subject }} {{ row.subjectName }}<template v-if="row.project">（{{ row.project }}）</template>
            </template>
          </el-table-column>
          <el-table-column label="借方" width="100" align="right">
            <template #default="{ row }">{{ row.direction === 'debit' ? fmt(row.amount) : '' }}</template>
          </el-table-column>
          <el-table-column label="贷方" width="100" align="right">
            <template #default="{ row }">{{ row.direction === 'credit' ? fmt(row.amount) : '' }}</template>
          </el-table-column>
        </el-table>
        <p class="hint" style="margin-top: 10px" v-if="detail.sourceType === 'voucher'">
          来源：业务凭证 #{{ detail.sourceId }}（作废业务凭证会级联作废本凭证）
        </p>
        <p class="hint" style="margin-top: 10px" v-if="detail.sourceType === 'agg'">
          来源：小区 #{{ detail.sourceId }} 当日全部业务单据汇总（业务增删后自动重建）
        </p>
        <p class="hint" style="margin-top: 10px" v-if="detail.status === 'normal' && detail.sourceType === 'manual'">
          手工凭证可在本页直接作废（留痕）
        </p>
      </div>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'
import { printVoucher } from '../print'

const month = ref('')
const status = ref('')
const list = ref([])
const drawer = ref(false)
const detail = ref(null)
const subjects = ref([])

const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const total = computed(() => list.value.filter((r) => r.status === 'normal').reduce((s, r) => s + Number(r.amount || 0), 0))

const SOURCE = { manual: '手工录入', voucher: '业务单据', agg: '汇总凭证', closing: '月末结转', opening: '期初建账' }
const sourceName = (t) => SOURCE[t] || t

function sumAmount({ columns, data }) {
  const sums = ['合计', '', '', '', 0, '']
  for (const r of data) if (r.status === 'normal') sums[4] += Number(r.amount || 0)
  return sums.map((v, i) => (i === 4 ? fmt(v) : v))
}

const loadFailed = ref(false)

async function load() {
  try {
    list.value = await api.get('/gl/vouchers', { params: { month: month.value || '', status: status.value || '' } })
    loadFailed.value = false
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载凭证列表失败：' + e.message)
  }
}

async function open(row) {
  try {
    detail.value = await api.get(`/gl/vouchers/${row.id}`)
    drawer.value = true
  } catch (e) { ElMessage.error('加载凭证详情失败：' + e.message) }
}

async function voidOne() {
  const d = detail.value
  if (!d || d.sourceType !== 'manual') return
  try {
    await ElMessageBox.confirm(`作废凭证 ${d.no}？作废留痕、不可恢复。`, '作废凭证', { type: 'warning' })
  } catch { return }
  try {
    await api.post(`/gl/vouchers/${d.id}/void`)
    ElMessage.success('已作废')
    drawer.value = false
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

function printOne() {
  const d = detail.value
  const subMap = {}
  for (const s of subjects.value) subMap[s.code] = s
  const parentName = (code) => {
    const s = subMap[code]
    if (!s || !s.parent) return s ? s.name : code
    const p = subMap[s.parent]
    return p ? p.name : s.name
  }
  const headSummary = d.summary.replace(/^手工凭证｜/, '')
  printVoucher({
    no: d.no,
    date: d.date,
    summary: headSummary,
    appendix: d.appendix || 0,
    entries: d.entries.map((e) => ({
      summary: e.summary || headSummary,
      subject: parentName(e.subject),
      sub: (subMap[e.subject] && subMap[e.subject].parent ? e.subjectName : '') + (e.project ? '（' + e.project + '）' : ''),
      direction: e.direction,
      amount: e.amount,
    })),
  })
}

async function init() {
  loadFailed.value = false
  try {
    subjects.value = await api.get('/gl/subjects')
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载科目失败：' + e.message)
  }
  await load()
}

onMounted(init)
</script>

<style scoped>
.filters { display: flex; gap: 12px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
.hint { color: #909399; font-size: 12px; }
</style>
