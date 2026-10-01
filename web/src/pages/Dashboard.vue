<template>
  <div>
    <div class="dash-head">
      <h2 style="margin: 0">首页</h2>
      <span class="today">今天：{{ todayText }}</span>
    </div>

    <!-- ==================== 日常录入快捷入口 ==================== -->
    <div class="quick-cards">
      <div class="quick-card income" @click="goDaily('income')">
        <div class="qc-icon">＋</div>
        <div class="qc-title">录入收款</div>
        <div class="qc-desc">住户缴纳维修基金</div>
      </div>
      <div class="quick-card expense" @click="goDaily('expense')">
        <div class="qc-icon">＋</div>
        <div class="qc-title">录入维修支出</div>
        <div class="qc-desc">录入维修项目，自动分摊到户</div>
      </div>
      <div class="quick-card interest" @click="goDaily('interest')">
        <div class="qc-icon">＋</div>
        <div class="qc-title">录入利息</div>
        <div class="qc-desc">录入银行利息</div>
      </div>
    </div>

    <!-- ==================== 统计 ==================== -->
    <el-row :gutter="14" style="margin-bottom: 18px">
      <el-col :span="4"><el-card><div class="label">基金总余额</div><div class="value">¥ {{ fmt(s.totalBalance) }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="label">今日收入（缴纳）</div><div class="value income">¥ {{ fmt(s.todayIncome) }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="label">今日支出</div><div class="value expense">¥ {{ fmt(s.todayExpense) }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="label">本月收入</div><div class="value income">¥ {{ fmt(s.monthIncome) }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="label">本月支出</div><div class="value expense">¥ {{ fmt(s.monthExpense) }}</div></el-card></el-col>
      <el-col :span="4"><el-card><div class="label">小区数 / 总户数</div><div class="value">{{ s.communities }} / {{ s.households }}</div></el-card></el-col>
    </el-row>

    <!-- ==================== 最近录入 ==================== -->
    <div class="panel">
      <h3>最近录入
        <el-button size="small" style="margin-left: 14px" @click="nav.page = 'vouchers'">查看全部凭证 →</el-button>
      </h3>
      <el-table :data="recent" size="small" border>
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag :type="tagType(row.type)" size="small">{{ TYPE_LABEL[row.type] || row.type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="对象" min-width="200">
          <template #default="{ row }">
            <template v-if="row.type === 'income'">{{ row.community }} {{ row.building }} {{ row.roomNo }} {{ row.owner }}</template>
            <template v-else-if="row.type === 'expense'">{{ row.community }}{{ row.building ? ' ' + row.building : '' }}（{{ row.summary || '维修项目' }}）</template>
            <template v-else-if="row.type === 'interest'">{{ row.community }} 公共账</template>
            <template v-else>{{ row.community }} {{ row.building }} {{ row.roomNo }} {{ row.owner }}</template>
          </template>
        </el-table-column>
        <el-table-column prop="summary" label="摘要" min-width="160" show-overflow-tooltip />
        <el-table-column label="金额" width="130" align="right">
          <template #default="{ row }">
            <span :style="{ color: signColor(row), fontWeight: 600 }">
              {{ sign(row) }}{{ fmt(row.amount) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'voided'" type="info" size="small">已作废</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel">
      <h3>年度汇总（全部年份）
        <el-button size="small" style="margin-left: 14px" @click="printYearly">打印年报</el-button>
        <el-button size="small" @click="exportYearly">导出 Excel</el-button>
      </h3>
      <el-table :data="yearly" size="small" border>
        <el-table-column prop="group" label="年度" width="110" />
        <el-table-column prop="income" label="缴纳收入" align="right" :formatter="moneyFmt" />
        <el-table-column prop="expense" label="维修支出" align="right" :formatter="moneyFmt" />
        <el-table-column prop="interest" label="利息" align="right" :formatter="moneyFmt" />
        <el-table-column prop="allocate" label="分摊到户" align="right" :formatter="moneyFmt" />
        <el-table-column prop="count" label="凭证数" width="90" align="right" />
      </el-table>
    </div>

    <div class="panel">
      <h3>月度汇总（近 24 个月）
        <el-button size="small" style="margin-left: 14px" @click="printMonthly">打印月报</el-button>
        <el-button size="small" @click="exportMonthly">导出 Excel</el-button>
      </h3>
      <el-table :data="monthly" size="small" border>
        <el-table-column prop="group" label="月份" width="110" />
        <el-table-column prop="income" label="缴纳收入" align="right" :formatter="moneyFmt" />
        <el-table-column prop="expense" label="维修支出" align="right" :formatter="moneyFmt" />
        <el-table-column prop="interest" label="利息" align="right" :formatter="moneyFmt" />
        <el-table-column prop="allocate" label="分摊到户" align="right" :formatter="moneyFmt" />
        <el-table-column prop="count" label="凭证数" width="90" align="right" />
      </el-table>
    </div>

    <div class="panel">
      <h3>每日汇总（近 30 天有发生额的日期）
        <el-button size="small" style="margin-left: 14px" @click="exportDaily">导出 Excel</el-button>
      </h3>
      <el-table :data="daily" size="small" border>
        <el-table-column prop="group" label="日期" width="110" />
        <el-table-column prop="income" label="缴纳收入" align="right" :formatter="moneyFmt" />
        <el-table-column prop="expense" label="维修支出" align="right" :formatter="moneyFmt" />
        <el-table-column prop="interest" label="利息" align="right" :formatter="moneyFmt" />
        <el-table-column prop="allocate" label="分摊到户" align="right" :formatter="moneyFmt" />
        <el-table-column prop="count" label="凭证数" width="90" align="right" />
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
import { printHTML, fmtMoney } from '../print'
import { exportExcel } from '../export'
import { nav, goDaily } from '../store'

const s = ref({})
const monthly = ref([])
const daily = ref([])
const yearly = ref([])
const recent = ref([])

const TYPE_LABEL = { income: '收款', expense: '维修支出', interest: '利息', allocate: '分摊到户' }
const tagType = (t) => ({ income: 'danger', expense: 'success', interest: 'primary', allocate: 'warning' }[t] || 'info')
const sign = (row) => (row.type === 'allocate' || row.type === 'expense' ? '−' : '+')
const signColor = (row) => (row.type === 'allocate' || row.type === 'expense' ? '#3b6d11' : '#a32d2d')

const todayText = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })

function fmt(n) {
  return Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}
const moneyFmt = (row, col, val) => fmt(val)

onMounted(async () => {
  s.value = await api.get('/stats/dashboard')
  monthly.value = await api.get('/summary/monthly')
  daily.value = await api.get('/summary/daily')
  yearly.value = await api.get('/summary/yearly')
  recent.value = await api.get('/vouchers', { params: { limit: 8 } })
})

function sumRows(rows, key) {
  return rows.reduce((t, r) => t + Number(r[key] || 0), 0)
}

function summaryTableHtml(rows, label) {
  return `
    <table>
      <tr><th>${label}</th><th class="num">缴纳收入</th><th class="num">维修支出</th><th class="num">利息</th><th class="num">分摊到户</th><th class="num">凭证数</th></tr>
      ${rows.map((r) => `<tr><td>${r.group}</td>
        <td class="num">${fmtMoney(r.income)}</td><td class="num">${fmtMoney(r.expense)}</td>
        <td class="num">${fmtMoney(r.interest)}</td><td class="num">${fmtMoney(r.allocate)}</td>
        <td class="num">${r.count}</td></tr>`).join('')}
      <tr><td><b>合计</b></td>
        <td class="num"><b>${fmtMoney(sumRows(rows, 'income'))}</b></td>
        <td class="num"><b>${fmtMoney(sumRows(rows, 'expense'))}</b></td>
        <td class="num"><b>${fmtMoney(sumRows(rows, 'interest'))}</b></td>
        <td class="num"><b>${fmtMoney(sumRows(rows, 'allocate'))}</b></td>
        <td class="num"><b>${rows.reduce((t, r) => t + Number(r.count || 0), 0)}</b></td></tr>
    </table>`
}

function printMonthly() {
  printHTML('维修基金收支月报', `
    <h1>住房维修基金收支月报（近 24 个月）</h1>
    <div class="meta">编制单位（代管）：住建局　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    ${summaryTableHtml(monthly.value, '月份')}
    <p style="font-size:12px">基金总余额：¥ ${fmtMoney(s.value.totalBalance)}；小区数：${s.value.communities}；总户数：${s.value.households}。</p>
    <div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>
  `)
}

function printYearly() {
  printHTML('维修基金收支年报', `
    <h1>住房维修基金收支年报</h1>
    <div class="meta">编制单位（代管）：住建局　打印时间：${new Date().toLocaleString('zh-CN')}</div>
    ${summaryTableHtml(yearly.value, '年度')}
    <p style="font-size:12px">截至打印日基金总余额：¥ ${fmtMoney(s.value.totalBalance)}；小区数：${s.value.communities}；总户数：${s.value.households}。</p>
    <p style="font-size:12px;color:#555">说明：分摊到户金额即计入各户账目的维修支出；余额 = 期初建账 + 缴纳收入 + 利息 − 分摊到户，实时汇总计算。</p>
    <div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>
  `)
}

const SUMMARY_HEADERS = ['期间', '缴纳收入', '维修支出', '利息', '分摊到户', '凭证数']
function summaryRows(rows) {
  return rows.map((r) => [r.group, r.income, r.expense, r.interest, r.allocate, r.count])
}
function summaryTotalRow(rows) {
  return ['合计', sumRows(rows, 'income'), sumRows(rows, 'expense'), sumRows(rows, 'interest'), sumRows(rows, 'allocate'),
    rows.reduce((t, r) => t + Number(r.count || 0), 0)]
}
function exportMonthly() {
  exportExcel('维修基金月度汇总.xlsx', '月度汇总', SUMMARY_HEADERS, [...summaryRows(monthly.value), summaryTotalRow(monthly.value)])
}
function exportYearly() {
  exportExcel('维修基金年度汇总.xlsx', '年度汇总', SUMMARY_HEADERS, [...summaryRows(yearly.value), summaryTotalRow(yearly.value)])
}
function exportDaily() {
  exportExcel('维修基金每日汇总.xlsx', '每日汇总', SUMMARY_HEADERS, [...summaryRows(daily.value), summaryTotalRow(daily.value)])
}
</script>

<style scoped>
.dash-head { display: flex; align-items: baseline; gap: 16px; margin-bottom: 16px; }
.today { font-size: 14px; color: #6a7280; }
.quick-cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; margin-bottom: 20px; }
.quick-card { background: #fff; border: 1px solid #e2e5ea; border-left-width: 5px; border-radius: 10px; padding: 20px 24px; cursor: pointer; transition: box-shadow .15s; }
.quick-card:hover { box-shadow: 0 4px 14px rgba(0,0,0,.1); }
.quick-card.income { border-left-color: #a32d2d; }
.quick-card.expense { border-left-color: #3b6d11; }
.quick-card.interest { border-left-color: #185fa5; }
.qc-icon { font-size: 26px; font-weight: 700; line-height: 1; margin-bottom: 10px; }
.quick-card.income .qc-icon { color: #a32d2d; }
.quick-card.expense .qc-icon { color: #3b6d11; }
.quick-card.interest .qc-icon { color: #185fa5; }
.qc-title { font-size: 18px; font-weight: 600; margin-bottom: 4px; }
.qc-desc { font-size: 13px; color: #6a7280; }
.label { font-size: 12px; color: #6a7280; margin-bottom: 6px; }
.value { font-size: 20px; font-weight: 600; }
.value.income { color: #a32d2d; }
.value.expense { color: #3b6d11; }
</style>
