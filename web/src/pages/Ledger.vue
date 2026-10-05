<template>
  <div>
    <h2 style="margin: 0 0 8px">四级账簿</h2>
    <div class="hint" style="margin-bottom: 14px">
      层级：一级总账 → 二级小区 → 三级楼洞 → 四级住户。点击小区行查看楼洞，点击楼洞行查看住户；小区公共账余额 = 利息收入等未分摊项。
    </div>

    <div class="panel">
      <h3>一级：总账 / 二级：小区</h3>
      <el-table :data="communities" size="small" border highlight-current-row @row-click="pickCommunity">
        <el-table-column prop="name" label="小区" min-width="140" />
        <el-table-column prop="householdCount" label="户数" width="80" align="right" />
        <el-table-column prop="householdsBalance" label="户账合计" align="right" :formatter="moneyFmt" />
        <el-table-column prop="publicBalance" label="公共账（利息等）" align="right" :formatter="moneyFmt" />
        <el-table-column prop="balance" label="小区余额" align="right" :formatter="moneyFmt">
          <template #default="{ row }"><b>{{ fmt(row.balance) }}</b></template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel" v-if="currentCommunity">
      <h3>
        三级：楼洞 —— {{ currentCommunity.name }}
        <el-date-picker v-model="stmtYear" type="year" value-format="YYYY" placeholder="对账年度"
          style="width: 110px; margin-left: 16px" :clearable="false" />
        <el-button size="small" style="margin-left: 8px" @click="printStatement">打印对账单</el-button>
        <el-button size="small" @click="exportStatement">导出对账单</el-button>
        <el-button size="small" style="margin-left: 8px" @click="printHouseholds">打印分户余额表</el-button>
        <el-button size="small" @click="exportHouseholds">导出分户余额表</el-button>
      </h3>
      <el-table :data="buildings" size="small" border highlight-current-row @row-click="pickBuilding">
        <el-table-column prop="name" label="楼洞" min-width="140" />
        <el-table-column prop="householdCount" label="户数" width="80" align="right" />
        <el-table-column label="楼洞余额" align="right">
          <template #default="{ row }"><b>{{ fmt(row.balance) }}</b></template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel" v-if="currentBuilding">
      <h3>四级：住户 —— {{ currentCommunity.name }} / {{ currentBuilding.name }}</h3>
      <el-table :data="households" size="small" border max-height="480">
        <el-table-column prop="roomNo" label="户号" width="100" />
        <el-table-column prop="owner" label="户主" width="120" />
        <el-table-column prop="area" label="建筑面积㎡" align="right" :formatter="numFmt" />
        <el-table-column prop="openingBalance" label="期初余额" align="right" :formatter="moneyFmt" />
        <el-table-column label="当前余额" align="right">
          <template #default="{ row }">
            <b>{{ fmt(row.balance) }}</b>
            <el-tooltip v-if="row.belowThreshold" placement="top"
              :content="`余额低于首期交存额（¥ ${fmt(row.firstPayment)}）的 30%，按规定应续筹`">
              <el-tag type="danger" size="small" style="margin-left: 6px">低于30%</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { printHTML, fmtMoney } from '../print'
import { exportExcel } from '../export'

const communities = ref([])
const buildings = ref([])
const households = ref([])
const currentCommunity = ref(null)
const currentBuilding = ref(null)
const stmtYear = ref(String(new Date().getFullYear()))

const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const moneyFmt = (row, col, val) => fmt(val)
const numFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

async function pickCommunity(row) {
  currentCommunity.value = row
  currentBuilding.value = null
  households.value = []
  try {
    buildings.value = await api.get('/ledger/buildings', { params: { communityId: row.id } })
  } catch (e) { ElMessage.error('加载楼洞账失败：' + e.message) }
}
async function pickBuilding(row) {
  currentBuilding.value = row
  try {
    households.value = await api.get('/ledger/households', { params: { buildingId: row.id } })
  } catch (e) { ElMessage.error('加载住户账失败：' + e.message) }
}

async function printHouseholds() {
  let rows
  try {
    rows = await api.get('/reports/households', { params: { communityId: currentCommunity.value.id } })
  } catch (e) { return ElMessage.error('加载分户余额失败：' + e.message) }
  const buildings = [...new Set(rows.map((r) => r.building))]
  let total = 0
  let warnCount = 0
  const sections = buildings.map((b) => {
    const list = rows.filter((r) => r.building === b)
    const sub = list.reduce((s, r) => s + Number(r.balance), 0)
    total += sub
    return `<h3 style="font-size:13px;margin:14px 0 6px">${b}（小计：¥ ${fmtMoney(sub)}）</h3>
      <table>
        <tr><th>户号</th><th>户主</th><th class="num">建筑面积㎡</th><th class="num">期初余额</th><th class="num">当前余额</th><th>续筹提示</th></tr>
        ${list.map((r) => {
          if (r.belowThreshold) warnCount++
          return `<tr><td>${r.roomNo}</td><td>${r.owner || ''}</td>
          <td class="num">${fmtMoney(r.area)}</td><td class="num">${fmtMoney(r.openingBalance)}</td>
          <td class="num">${fmtMoney(r.balance)}</td>
          <td>${r.belowThreshold ? '<b style="color:#a32d2d">低于首期30%</b>' : ''}</td></tr>`
        }).join('')}
      </table>`
  }).join('')
  printHTML(`分户余额表 ${currentCommunity.value.name}`, `
    <h1>住房维修基金分户余额表</h1>
    <div class="meta">小区：${currentCommunity.value.name}　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    ${sections}
    <h3 style="font-size:13px;margin:14px 0 6px">合计（含公共账）：¥ ${fmtMoney(currentCommunity.value.balance)}　其中户账合计：¥ ${fmtMoney(total)}　公共账（利息等）：¥ ${fmtMoney(currentCommunity.value.publicBalance)}</h3>
    <p style="font-size:12px;color:#a32d2d">低于首期交存额30%红线的住户共 ${warnCount} 户（按165号令应及时续筹）。</p>
    <div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>
  `)
}

async function exportHouseholds() {
  let rows
  try {
    rows = await api.get('/reports/households', { params: { communityId: currentCommunity.value.id } })
  } catch (e) { return ElMessage.error('加载分户余额失败：' + e.message) }
  const table = rows.map((r) => [r.building, r.roomNo, r.owner || '', r.area, r.openingBalance, r.balance,
    r.belowThreshold ? '低于首期30%' : ''])
  const total = rows.reduce((s, r) => s + Number(r.balance), 0)
  table.push(['合计', '', '', '', '', total, ''])
  exportExcel(`分户余额表 ${currentCommunity.value.name}.xlsx`, '分户余额表',
    ['楼洞', '户号', '户主', '建筑面积㎡', '期初余额', '当前余额', '续筹提示'], table)
}

async function fetchStatement() {
  return api.get('/reports/community-statement', {
    params: { communityId: currentCommunity.value.id, year: stmtYear.value },
  })
}

function statementTableHtml(d) {
  return `
    <table>
      <tr><th colspan="2">小区对账单（${d.year} 年度）</th></tr>
      <tr><td width="40%"><b>小区名称</b></td><td>${d.community}</td></tr>
      <tr><td><b>对账期间</b></td><td>${d.year}-01-01 至 ${d.year}-12-31</td></tr>
      <tr><td><b>期初余额</b>（${d.year} 年 1 月 1 日）</td><td class="num">¥ ${fmtMoney(d.opening)}</td></tr>
      <tr><td><b>本期缴纳收入</b></td><td class="num">+ ¥ ${fmtMoney(d.income)}</td></tr>
      <tr><td><b>本期利息收入（公共账）</b></td><td class="num">+ ¥ ${fmtMoney(d.interest)}</td></tr>
      <tr><td><b>本期其他收入（经营/处置等）</b></td><td class="num">+ ¥ ${fmtMoney(d.fundIncome)}</td></tr>
      <tr><td><b>本期维修工程支出</b>（分摊到户总额与之一致）</td><td class="num">¥ ${fmtMoney(d.expense)}</td></tr>
      <tr><td><b>本期返还 / 退返</b></td><td class="num">− ¥ ${fmtMoney(d.refund)}</td></tr>
      <tr><td><b>本期分摊到户</b></td><td class="num">− ¥ ${fmtMoney(d.allocate)}</td></tr>
      <tr><td><b>期末余额</b></td><td class="num" style="font-size:14px"><b>¥ ${fmtMoney(d.closing)}</b></td></tr>
    </table>
    ${d.months.length ? `
    <h3 style="font-size:13px;margin:14px 0 6px">分月发生额</h3>
    <table>
      <tr><th>月份</th><th class="num">缴纳收入</th><th class="num">维修支出</th><th class="num">利息</th><th class="num">利息分配</th><th class="num">返还/退返</th><th class="num">分摊到户</th><th class="num">凭证数</th></tr>
      ${d.months.map((m) => `<tr><td>${m.month}</td>
        <td class="num">${fmtMoney(m.income)}</td><td class="num">${fmtMoney(m.expense)}</td>
        <td class="num">${fmtMoney(m.interest)}</td><td class="num">${fmtMoney(m.interestAlloc)}</td>
        <td class="num">${fmtMoney(m.refund)}</td><td class="num">${fmtMoney(m.allocate)}</td>
        <td class="num">${m.count}</td></tr>`).join('')}
    </table>` : ''}
    <p style="font-size:12px;color:#555">说明：期末余额 = 期初余额 + 缴纳收入 + 利息 + 其他收入 − 维修支出 − 返还/退返；维修工程支出通过分摊由各户维修基金承担，收益分配为公共账转入各户分户账的内部结转。</p>`
}

async function printStatement() {
  let d
  try {
    d = await fetchStatement()
  } catch (e) { return ElMessage.error('加载对账单失败：' + e.message) }
  printHTML(`小区对账单 ${d.community} ${d.year}`, `
    <h1>住房维修基金小区对账单</h1>
    <div class="meta">编制单位（代管）：住建局　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    ${statementTableHtml(d)}
    <div class="sign"><span>住建局（代管方）</span><span>业委会/物业确认</span><span>经办人</span><span>日期</span></div>
  `)
}

async function exportStatement() {
  let d
  try {
    d = await fetchStatement()
  } catch (e) { return ElMessage.error('加载对账单失败：' + e.message) }
  const rows = [
    ['小区名称', d.community],
    ['对账期间', `${d.year}-01-01 至 ${d.year}-12-31`],
    ['期初余额', d.opening],
    ['本期缴纳收入', d.income],
    ['本期利息收入（公共账）', d.interest],
    ['本期其他收入（经营/处置等）', d.fundIncome],
    ['本期维修工程支出', d.expense],
    ['本期返还/退返', d.refund],
    ['本期分摊到户', d.allocate],
    ['期末余额', d.closing],
    [],
    ['分月发生额'],
    ['月份', '缴纳收入', '维修支出', '利息', '利息分配', '返还/退返', '分摊到户', '凭证数'],
    ...d.months.map((m) => [m.month, m.income, m.expense, m.interest, m.interestAlloc, m.refund, m.allocate, m.count]),
  ]
  exportExcel(`小区对账单 ${d.community} ${d.year}.xlsx`, '小区对账单', ['项目', '金额/内容'], rows)
}

onMounted(async () => {
  try {
    communities.value = await api.get('/ledger/communities')
  } catch (e) { ElMessage.error('加载小区账失败：' + e.message) }
})
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
</style>
