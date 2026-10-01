<template>
  <div>
    <h2 style="margin: 0 0 16px">月结锁账</h2>
    <div class="panel">
      <h3>执行月结</h3>
      <div style="display: flex; gap: 10px; align-items: center; margin-bottom: 8px">
        <el-date-picker v-model="month" type="month" value-format="YYYY-MM" placeholder="选择月份" style="width: 160px" />
        <el-button type="primary" @click="close">月结该月</el-button>
      </div>
      <div class="hint">
        月结后该月全部凭证将被锁死：不能新增、不能作废、不能上传附件；需要改动时先对当月「反结转」。月结会生成该月发生额汇总，用于年度结转。
        <b>月结完成后系统自动生成当月月报表</b>（汇总表 / 收入明细 / 支出明细 / 分楼栋结余表，含活公式，可在下方下载）。
      </div>
    </div>

    <div class="panel">
      <h3>月报表（Excel，4 个子表）
        <el-button size="small" style="margin-left: 14px" @click="loadReports">刷新</el-button>
      </h3>
      <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
        <el-date-picker v-model="reportMonth" type="month" value-format="YYYY-MM" placeholder="选择月份" style="width: 160px" />
        <el-button size="small" type="primary" @click="genReport">生成 / 重新生成</el-button>
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

    <div class="panel">
      <h3>已月结月份</h3>
      <el-table :data="periods" size="small" border>
        <el-table-column prop="month" label="月份" width="140" />
        <el-table-column prop="closedAt" label="月结时间" width="200">
          <template #default="{ row }">{{ (row.closedAt || '').replace('T', ' ').slice(0, 19) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <el-button type="warning" size="small" link @click="reopen(row)">反结转</el-button>
            <el-button size="small" link @click="printPeriod(row)">打印月报</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!periods.length" class="hint" style="margin-top: 8px">还没有月结过任何月份</div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { printHTML, fmtMoney } from '../print'

const month = ref('')
const periods = ref([])
const reportMonth = ref('')
const reports = ref([])

async function load() {
  periods.value = await api.get('/periods')
}
async function loadReports() {
  reports.value = await api.get('/reports/monthly')
}
async function genReport() {
  if (!reportMonth.value) return ElMessage.warning('请选择月份')
  try {
    const res = await api.post('/reports/monthly', { month: reportMonth.value })
    ElMessage.success(`月报表已生成：${res.name}`)
    loadReports()
  } catch (e) { ElMessage.error(e.message) }
}
function downloadReport(row) {
  window.open(`/api/reports/monthly/file?name=${encodeURIComponent(row.name)}`, '_blank')
}

async function close() {
  if (!month.value) return ElMessage.warning('请选择月份')
  try {
    await ElMessageBox.confirm(
      `确定对 ${month.value} 月结吗？月结后该月凭证将锁定。`, '月结确认', { type: 'warning' })
  } catch { return }
  try {
    const res = await api.post('/periods/close', { month: month.value })
    if (res.reportError) ElMessage.warning(`${month.value} 月结完成，但${res.reportError}`)
    else if (res.report) ElMessage.success(`${month.value} 月结完成，月报表已自动生成：${res.report}`)
    else ElMessage.success(`${month.value} 月结完成`)
    load(); loadReports()
  } catch (e) { ElMessage.error(e.message) }
}

async function reopen(row) {
  try {
    await ElMessageBox.confirm(
      `反结转 ${row.month} 后该月凭证恢复可改，确定继续吗？`, '反结转确认', { type: 'warning' })
  } catch { return }
  try {
    await api.post('/periods/reopen', { month: row.month })
    ElMessage.success(`${row.month} 已反结转`)
    load()
  } catch (e) { ElMessage.error(e.message) }
}

async function printPeriod(row) {
  const [monthly, stats] = await Promise.all([
    api.get('/summary/monthly'),
    api.get('/stats/dashboard'),
  ])
  const m = monthly.find((x) => x.group === row.month)
  printHTML(`维修基金月度汇总表 ${row.month}`, `
    <h1>住房维修基金月度汇总表</h1>
    <div class="meta">月份：${row.month}　编制单位（代管）：住建局　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    <table>
      <tr><th>缴纳收入</th><th>维修支出</th><th>利息收入</th><th>分摊到户</th><th>凭证数</th></tr>
      ${m ? `<tr>
        <td class="num">${fmtMoney(m.income)}</td><td class="num">${fmtMoney(m.expense)}</td>
        <td class="num">${fmtMoney(m.interest)}</td><td class="num">${fmtMoney(m.allocate)}</td>
        <td class="num">${m.count}</td></tr>` : '<tr><td colspan="5" style="text-align:center">该月无凭证</td></tr>'}
    </table>
    <p style="font-size:12px">截至当前，基金总余额：¥ ${fmtMoney(stats.totalBalance)}；总户数：${stats.households}。</p>
    <div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>
  `)
}

onMounted(() => { load(); loadReports() })
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; line-height: 1.7; }
</style>
