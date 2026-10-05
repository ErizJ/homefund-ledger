<template>
  <div>
    <h2 style="margin: 0 0 16px">期末业务</h2>
    <ReloadBanner :failed="loadFailed" @retry="init" />

    <!-- 期间与操作 -->
    <div class="panel">
      <h3>期间操作</h3>
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap">
        <el-date-picker v-model="month" type="month" value-format="YYYY-MM" placeholder="选择月份" style="width: 160px" :clearable="false" />
        <el-button v-if="!isClosed" type="primary" :loading="busy" @click="doTransfer">月末结转</el-button>
        <el-button v-else type="info" :loading="busy" @click="reopen">反结转</el-button>
        <el-tag v-if="isClosed" type="success" size="small">该月已结转</el-tag>
        <span class="hint" v-else>该月未结转</span>
      </div>
      <div class="hint" style="margin-top: 8px">
        月末结转：<b>交存收入 → 商品住宅/已售公有住房维修资金（净资产）</b>；利息等收益（存款利息、国债利息、经营、处置、其他收入）→ 待分配累计收益；
        维修支出、返还支出、其他支出 → 维修资金（净资产）。结转凭证自动生成，可重复执行（重新结转时按最新账目重算），并生成当月月报表与财务报表快照。<br />
        不锁账：结转后仍可记账/作废，改账后该月自动恢复"未结转"状态，点「月末结转」重新结转即可。反结转 = 撤销该月结转凭证。
      </div>
    </div>

    <!-- 年度操作（年末结转） -->
    <div class="panel">
      <h3>年度操作（年末结转）
        <el-tag v-if="isYearLocked" type="success" size="small" style="margin-left: 8px">{{ curYear }} 已年度结转</el-tag>
      </h3>
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap">
        <el-date-picker v-model="curYear" type="year" value-format="YYYY" placeholder="选择年度" style="width: 120px" :clearable="false" />
        <el-button v-if="!isYearLocked" type="primary" :loading="busy" @click="closeYear">年度结转</el-button>
        <el-button v-else type="info" :loading="busy" @click="reopenYear">反年度结转</el-button>
      </div>
      <div class="hint" style="margin-top: 8px">
        年度结转：一键结转年内所有有收支的月份（幂等，按最新账目重算），并生成年度财务报表快照（会住维01/02/03表，本年数/上年数口径）。<br />
        不锁账：年度结转后仍可改账，改账后年度自动恢复"未结转"状态，重新点「年度结转」即可。反年度结转 = 撤销全年结转凭证。
      </div>
      <div v-if="years.length" style="margin-top: 10px">
        <span class="hint">已年结年度：</span>
        <el-tag v-for="y in years" :key="y.year" size="small" style="margin-right: 6px">
          {{ y.year }}（{{ (y.closedAt || '').replace('T', ' ').slice(0, 16) }}）
        </el-tag>
      </div>
    </div>

    <!-- 科目汇总表（记账凭证汇总） -->
    <div class="panel">
      <h3>记账凭证汇总表（科目汇总）
        <el-radio-group v-model="sumMode" size="small" style="margin-left: 16px" @change="loadSummary">
          <el-radio-button value="month">按月</el-radio-button>
          <el-radio-button value="year">按年</el-radio-button>
        </el-radio-group>
        <el-button size="small" style="margin-left: 12px" @click="printSummary">打印</el-button>
        <el-button size="small" @click="exportSummary">导出 Excel</el-button>
        <el-button size="small" @click="loadSummary">刷新</el-button>
      </h3>
      <div class="hint" style="margin-bottom: 10px">
        月结 / 年结后系统自动按一级科目汇总本期借贷发生额；也可随时查看任一期间的汇总。
        <template v-if="summary">
          当前期间 {{ summary.title }}：共 {{ summary.voucherCount }} 张凭证，
          借方合计 ¥ {{ fmt(summary.totalDebit) }} = 贷方合计 ¥ {{ fmt(summary.totalCredit) }}
          <el-tag size="small" :type="summary.balanced ? 'success' : 'danger'">{{ summary.balanced ? '借贷平衡' : '不平' }}</el-tag>
        </template>
      </div>
      <el-table :data="summaryRows" size="default" border :show-summary="summaryRows.length > 0" :summary-method="sumSummary">
        <el-table-column prop="code" label="科目编码" width="110" />
        <el-table-column prop="name" label="科目名称" min-width="220" />
        <el-table-column label="借方发生额" align="right" width="170">
          <template #default="{ row }">{{ fmt(row.debit) }}</template>
        </el-table-column>
        <el-table-column label="贷方发生额" align="right" width="170">
          <template #default="{ row }">{{ fmt(row.credit) }}</template>
        </el-table-column>
        <el-table-column prop="count" label="凭证张数" width="100" align="right" />
      </el-table>
    </div>

    <!-- 月报表 Excel -->
    <div class="panel">
      <h3>月报表（Excel，4 个子表：汇总表 / 收入明细 / 支出明细 / 分楼栋结余表）
        <el-button size="small" style="margin-left: 14px" @click="loadReports">刷新</el-button>
      </h3>
      <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
        <el-date-picker v-model="reportMonth" type="month" value-format="YYYY-MM" placeholder="选择月份" style="width: 160px" />
        <el-button size="small" type="primary" :loading="busy" @click="genReport">生成 / 重新生成</el-button>
        <span class="hint">反结转改账后，请重新生成对应月份的报表</span>
      </div>
      <el-table :data="reports" size="small" border>
        <el-table-column prop="name" label="文件名" min-width="240" />
        <el-table-column prop="modified" label="生成时间" width="170" />
        <el-table-column label="大小" width="100">
          <template #default="{ row }">{{ (row.size / 1024).toFixed(1) }} KB</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="downloadReport(row)">下载</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!reports.length" class="hint" style="margin-top: 8px">还没有生成过月报表</div>
    </div>

    <!-- 已月结月份 -->
    <div class="panel">
      <h3>已结转月份</h3>
      <el-table :data="periods" size="small" border>
        <el-table-column prop="month" label="月份" width="140" />
        <el-table-column prop="closedAt" label="结转时间" width="200">
          <template #default="{ row }">{{ (row.closedAt || '').replace('T', ' ').slice(0, 19) }}</template>
        </el-table-column>
      </el-table>
      <div v-if="!periods.length" class="hint" style="margin-top: 8px">还没有月结过任何月份</div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'
import { printHTML } from '../print'
import { exportExcel } from '../export'

const month = ref(new Date().toISOString().slice(0, 7))
const periods = ref([])
const sumMode = ref('month')
const summary = ref(null)
const reportMonth = ref('')
const reports = ref([])
const busy = ref(false) // 期间/年度操作按钮 loading（防连点）
const curYear = ref(String(new Date().getFullYear()))
const years = ref([])

const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const isClosed = computed(() => periods.value.some((p) => p.month === month.value))
const isYearLocked = computed(() => years.value.some((y) => y.year === curYear.value))
const summaryRows = computed(() => (summary.value ? summary.value.rows : []))

const loadFailed = ref(false)
const failLoad = (label) => (e) => {
  loadFailed.value = true
  ElMessage.error(label + '：' + e.message)
}

async function loadPeriods() {
  try { periods.value = await api.get('/periods') } catch (e) { failLoad('加载结转记录失败')(e) }
}
async function loadYears() {
  try { years.value = await api.get('/periods/years') } catch (e) { failLoad('加载年度结转记录失败')(e) }
}
async function loadReports() {
  try { reports.value = await api.get('/reports/monthly') } catch (e) { failLoad('加载月报表列表失败')(e) }
}

// 期间/年度操作统一包装：loading + 成功/失败提示由各调用方完成
async function withBusy(fn) {
  if (busy.value) return
  busy.value = true
  try {
    await fn()
  } finally {
    busy.value = false
  }
}
async function loadSummary() {
  try {
    const params = sumMode.value === 'month' ? { month: month.value } : { year: month.value.slice(0, 4) }
    summary.value = await api.get('/gl/voucher-summary', { params })
  } catch (e) { failLoad('加载凭证汇总失败')(e) }
}

function sumSummary({ columns, data }) {
  const sums = ['合计', '', 0, 0, 0]
  for (const r of data) { sums[2] += r.debit; sums[3] += r.credit; sums[4] += r.count }
  return sums.map((v, i) => (i >= 2 ? fmt(v) : v))
}

async function doTransfer() {
  try {
    await ElMessageBox.confirm(
      `将 ${month.value} 的收入类科目余额（交存收入、利息收入等）与支出类科目余额结转入净资产，生成一张结转凭证。该操作不锁账，之后仍可继续记账（再次结转只处理新增部分）。是否执行？`,
      `月末结转 ${month.value}`, { confirmButtonText: '确认结转', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  await withBusy(async () => {
    try {
      const res = await api.post('/gl/transfer', { month: month.value })
      ElMessage.success(`月末结转成功！结转凭证 ${res.no} 已生成并过账`)
      loadSummary()
    } catch (e) { ElMessage.error('月末结转失败：' + e.message) }
  })
}

async function reopen() {
  try {
    await ElMessageBox.confirm(
      `反结转 ${month.value}：撤销该月结转凭证，收入/支出余额恢复。确定继续吗？`, '反结转确认', { type: 'warning' })
  } catch { return }
  await withBusy(async () => {
    try {
      await api.post('/periods/reopen', { month: month.value })
      ElMessage.success(`${month.value} 反结转成功，该月恢复未结转状态`)
      loadPeriods(); loadSummary()
    } catch (e) { ElMessage.error('反结转失败：' + e.message) }
  })
}

// ==================== 年度操作（年末结转） ====================

async function closeYear() {
  try {
    await ElMessageBox.confirm(
      `对 ${curYear.value} 年度执行年度结转：一键结转年内所有有收支的月份（不锁账，改账后需重新结转），并生成年度财务报表快照。是否执行？`,
      `${curYear.value} 年度结转确认`, { confirmButtonText: '确认结转', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  await withBusy(async () => {
    try {
      const res = await api.post('/periods/close-year', { year: curYear.value })
      let msg = `${curYear.value} 年度结转成功`
      if (res.transferredMonths) msg += `（已结转月份：${res.transferredMonths}）`
      if (res.statementReport) msg += `；年度财务报表快照：${res.statementReport}`
      if (res.statementReportError) {
        msg += `；注意：${res.statementReportError}`
        ElMessage.warning(msg)
      } else ElMessage.success(msg, { duration: 8000 })
      loadYears(); loadPeriods(); loadReports()
    } catch (e) { ElMessage.error('年度结转失败：' + e.message) }
  })
}

async function reopenYear() {
  try {
    await ElMessageBox.confirm(
      `反年度结转 ${curYear.value}：撤销全年结转凭证，收入/支出余额恢复。确定继续吗？`, '反年度结转确认', { type: 'warning' })
  } catch { return }
  await withBusy(async () => {
    try {
      await api.post('/periods/reopen-year', { year: curYear.value })
      ElMessage.success(`${curYear.value} 反年度结转成功，全年恢复未结转状态`)
      loadYears(); loadPeriods()
    } catch (e) { ElMessage.error('反年度结转失败：' + e.message) }
  })
}

function printSummary() {
  if (!summary.value) return
  const rowsHtml = summary.value.rows.map((r) =>
    `<tr><td>${r.code}</td><td>${r.name}</td><td class="num">${fmt(r.debit)}</td><td class="num">${fmt(r.credit)}</td><td class="num">${r.count}</td></tr>`).join('')
  printHTML(`记账凭证汇总表（${summary.value.title}）`, `
    <h1>记账凭证汇总表</h1>
    <div class="meta">期间：${summary.value.title}　凭证张数：${summary.value.voucherCount}　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    <table>
      <tr><th>科目编码</th><th>科目名称</th><th>借方发生额</th><th>贷方发生额</th><th>凭证张数</th></tr>
      ${rowsHtml}
      <tr style="font-weight:600"><td colspan="2">合　计</td><td class="num">${fmt(summary.value.totalDebit)}</td><td class="num">${fmt(summary.value.totalCredit)}</td><td class="num">${summary.value.voucherCount}</td></tr>
    </table>
    <div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>`)
}

function exportSummary() {
  if (!summary.value) return
  const rows = [[
    ...summary.value.rows.map((r) => [r.code, r.name, r.debit, r.credit, r.count]),
    ['', '合计', summary.value.totalDebit, summary.value.totalCredit, summary.value.voucherCount],
  ]]
  exportExcel(`记账凭证汇总表-${summary.value.title}.xlsx`, '科目汇总',
    ['科目编码', '科目名称', '借方发生额', '贷方发生额', '凭证张数'], rows)
}

async function genReport() {
  const m = reportMonth.value || month.value
  // 同月已有报表时属于覆盖重生成，先确认（文件名含月份）
  const exist = reports.value.some((r) => (r.name || '').includes(m))
  if (exist) {
    try {
      await ElMessageBox.confirm(
        `${m} 已生成过月报表，重新生成将覆盖原文件。确认覆盖？`, '重新生成月报表',
        { type: 'warning', confirmButtonText: '确认覆盖', cancelButtonText: '取消' })
    } catch { return }
  }
  await withBusy(async () => {
    try {
      const res = await api.post('/reports/monthly', { month: m })
      ElMessage.success(`月报表生成成功：${res.name}`)
      await loadReports()
    } catch (e) { ElMessage.error('月报表生成失败：' + e.message) }
  })
}
function downloadReport(row) {
  window.open(`/api/reports/monthly/file?name=${encodeURIComponent(row.name)}`, '_blank')
}

async function init() {
  loadFailed.value = false
  await Promise.all([loadPeriods(), loadYears(), loadReports(), loadSummary()])
}

onMounted(init)
</script>

<style scoped>
.hint { color: #909399; font-size: 12px; line-height: 1.7; }
</style>
