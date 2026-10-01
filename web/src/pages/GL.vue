<template>
  <div>
    <h2 style="margin: 0 0 16px">财务账套（财会〔2020〕7号 · 收付实现制 · 借贷记账法）</h2>

    <el-tabs v-model="tab">
      <!-- ==================== 科目余额表 ==================== -->
      <el-tab-pane label="科目余额表" name="balances">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
          <el-date-picker v-model="bMonth" type="month" value-format="YYYY-MM" style="width: 150px"
            :clearable="false" @change="loadBalances" />
          <span class="hint">截至所选月末的累计发生额与余额（含项目辅助核算展开）</span>
        </div>
        <el-table :data="balanceRows" size="small" border :default-expand-all="false" row-key="key">
          <el-table-column prop="code" label="科目编码" width="110" />
          <el-table-column prop="name" label="科目名称" min-width="180" />
          <el-table-column prop="project" label="项目（小区）" min-width="120" />
          <el-table-column label="借方发生" align="right" :formatter="moneyFmt" prop="debit" />
          <el-table-column label="贷方发生" align="right" :formatter="moneyFmt" prop="credit" />
          <el-table-column label="余额方向" width="90" align="center" prop="balanceDir">
            <template #default="{ row }">{{ row.balanceDir === 'debit' ? '借' : '贷' }}</template>
          </el-table-column>
          <el-table-column label="余额" align="right" :formatter="moneyFmt" prop="balance" />
        </el-table>
      </el-tab-pane>

      <!-- ==================== 总账（总分类账） ==================== -->
      <el-tab-pane label="总账" name="generalLedger">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
          <el-date-picker v-model="gMonth" type="month" value-format="YYYY-MM" style="width: 150px"
            :clearable="false" @change="loadGLedger" />
          <el-button size="small" @click="printGLedger">打印总账</el-button>
          <span class="hint">三栏式总账：记账凭证保存后自动过账计入，收入/支出类科目月末结转时并入净资产</span>
        </div>
        <el-table :data="gRows" size="default" border show-summary :summary-method="gSummary">
          <el-table-column prop="code" label="科目编码" width="100" />
          <el-table-column prop="name" label="科目名称" min-width="190" />
          <el-table-column label="方向" width="60" align="center">
            <template #default="{ row }">{{ row.dir === 'debit' ? '借' : '贷' }}</template>
          </el-table-column>
          <el-table-column label="期初余额" align="right" :formatter="moneyFmt" prop="opening" />
          <el-table-column label="本期借方发生" align="right" :formatter="moneyFmt" prop="debit" />
          <el-table-column label="本期贷方发生" align="right" :formatter="moneyFmt" prop="credit" />
          <el-table-column label="期末余额" align="right" :formatter="moneyFmt" prop="closing" />
        </el-table>
        <div class="hint" style="margin-top: 10px">
          月末结转在「月结锁账」页执行：结转后收入类科目余额清零、并入净资产（3001/3002），与业务台账对账线保持勾稽。
        </div>
      </el-tab-pane>

      <!-- ==================== 明细账 ==================== -->
      <el-tab-pane label="明细账" name="entries">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap">
          <el-select v-model="eSubject" placeholder="选择科目（可输编码过滤）" filterable style="width: 260px" clearable @change="loadEntries">
            <el-option v-for="s in subjects" :key="s.code" :label="s.code + ' ' + s.name" :value="s.code" />
          </el-select>
          <el-select v-model="eProject" placeholder="全部小区" style="width: 160px" clearable @change="loadEntries">
            <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
          </el-select>
          <el-date-picker v-model="eFrom" type="date" value-format="YYYY-MM-DD" placeholder="开始日期" style="width: 140px" @change="loadEntries" />
          <el-date-picker v-model="eTo" type="date" value-format="YYYY-MM-DD" placeholder="结束日期" style="width: 140px" @change="loadEntries" />
        </div>
        <el-table :data="entries" size="small" border>
          <el-table-column prop="date" label="日期" width="100" />
          <el-table-column prop="no" label="凭证号" width="150" />
          <el-table-column prop="subject" label="科目" width="90" />
          <el-table-column prop="subjectName" label="科目名称" min-width="150" />
          <el-table-column prop="project" label="项目（小区）" width="110" />
          <el-table-column prop="summary" label="摘要" min-width="180" />
          <el-table-column label="方向" width="60" align="center">
            <template #default="{ row }">
              <el-tag :type="row.direction === 'debit' ? 'danger' : 'success'" size="small">
                {{ row.direction === 'debit' ? '借' : '贷' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="金额" align="right" :formatter="moneyFmt" prop="amount" />
        </el-table>
      </el-tab-pane>

      <!-- ==================== 记账凭证（财务账） ==================== -->
      <el-tab-pane label="记账凭证" name="vouchers">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
          <el-select v-model="vMonth" placeholder="全部月份" style="width: 150px" clearable @change="loadGlVouchers">
            <el-option v-for="m in closedMonths" :key="m" :label="m" :value="m" />
          </el-select>
          <span class="hint">业务单据自动生成（GL 前缀）· 月末结转（JZ 前缀）· 期初建账（OPEN 前缀除外，见摘要）</span>
        </div>
        <el-table :data="glVouchers" size="small" border @row-click="showGlVoucher" highlight-current-row>
          <el-table-column prop="no" label="凭证号" width="170" />
          <el-table-column prop="date" label="日期" width="100" />
          <el-table-column label="类型" width="90" align="center">
            <template #default="{ row }">
              <el-tag size="small" :type="{ business: 'primary', closing: 'warning', opening: 'success' }[row.kind]">
                {{ { business: '业务', closing: '结转', opening: '期初' }[row.kind] }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="summary" label="摘要" min-width="220" />
          <el-table-column prop="lines" label="分录数" width="80" align="right" />
          <el-table-column label="状态" width="80" align="center">
            <template #default="{ row }">{{ row.status === 'normal' ? '正常' : '已作废' }}</template>
          </el-table-column>
        </el-table>

        <el-drawer v-model="drawer" title="记账凭证详情" size="480px">
          <div v-if="glDetail">
            <p style="margin-top:0"><b>{{ glDetail.no }}</b>　{{ glDetail.date }}　{{ glDetail.summary }}
              <el-button size="small" type="primary" style="float: right" @click="printGlVoucher">打印记帐凭证</el-button>
            </p>
            <el-table :data="glDetail.entries" size="small" border>
              <el-table-column label="摘要" min-width="140">
                <template #default="{ row }">{{ row.direction === 'debit' ? '借' : '贷' }}：{{ row.subjectName }}</template>
              </el-table-column>
              <el-table-column prop="subject" label="科目" width="80" />
              <el-table-column prop="project" label="项目" width="100" />
              <el-table-column label="金额" align="right" :formatter="moneyFmt" prop="amount" />
            </el-table>
            <p class="hint" style="margin-top: 10px" v-if="glDetail.sourceType === 'voucher'">
              来源：业务凭证 #{{ glDetail.sourceId }}（作废业务凭证会级联作废本凭证）
            </p>
          </div>
        </el-drawer>
      </el-tab-pane>

      <!-- ==================== 填制凭证（手工单据） ==================== -->
      <el-tab-pane label="填制凭证" name="create">
        <div class="panel">
          <h3>填制记账凭证</h3>
          <div class="hint" style="margin-bottom: 12px">
            像填纸质凭证一样：先写日期和摘要，再逐行填分录——方向（借/贷）、科目、项目（小区）、金额。
            系统实时校验借贷平衡，保存后立即过账，余额表/明细账/试算同步生效。
          </div>
          <el-form inline>
            <el-form-item label="日期">
              <el-date-picker v-model="mDate" type="date" value-format="YYYY-MM-DD" style="width: 140px" :clearable="false" />
            </el-form-item>
            <el-form-item label="摘要">
              <el-input v-model="mSummary" style="width: 320px" placeholder="如：业主张三交存维修基金" />
            </el-form-item>
          </el-form>
          <el-table :data="mEntries" size="small" border>
            <el-table-column label="方向" width="90" align="center">
              <template #default="{ row }">
                <el-select v-model="row.direction" style="width: 70px">
                  <el-option label="借" value="debit" />
                  <el-option label="贷" value="credit" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="科目" min-width="240">
              <template #default="{ row }">
                <el-select v-model="row.subjectCode" filterable placeholder="选择科目" style="width: 100%">
                  <el-option v-for="s in subjects" :key="s.code" :label="s.code + ' ' + s.name" :value="s.code" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="项目（小区）" min-width="160">
              <template #default="{ row }">
                <el-select v-model="row.projectId" clearable placeholder="无" style="width: 100%">
                  <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column label="金额（元）" width="170">
              <template #default="{ row }">
                <el-input-number v-model="row.amount" :min="0" :precision="2" :controls="false" style="width: 140px" placeholder="0.00" />
              </template>
            </el-table-column>
            <el-table-column label="" width="70" align="center">
              <template #default="{ $index }">
                <el-button link type="danger" size="small" @click="mEntries.splice($index, 1)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div style="margin-top: 12px; display: flex; gap: 16px; align-items: center">
            <el-button size="small" @click="addEntryRow">+ 添加分录</el-button>
            <span>借方合计：<b>¥ {{ fmt(mDebit) }}</b></span>
            <span>贷方合计：<b>¥ {{ fmt(mCredit) }}</b></span>
            <el-tag size="small" :type="mBalanced ? 'success' : 'danger'">{{ mBalanced ? '借贷平衡' : '借贷不平' }}</el-tag>
            <el-button type="primary" :disabled="!mBalanced" @click="saveManual">保存凭证</el-button>
          </div>
        </div>
      </el-tab-pane>

      <!-- ==================== 试算平衡与对账 ==================== -->
      <el-tab-pane label="试算与对账" name="reconcile">
        <div class="panel" style="margin-bottom: 14px">
          <h3>试算平衡</h3>
          <div v-if="trial" style="display: flex; gap: 30px; align-items: baseline">
            <span>借方合计：<b>¥ {{ fmt(trial.debit) }}</b></span>
            <span>贷方合计：<b>¥ {{ fmt(trial.credit) }}</b></span>
            <el-tag :type="trial.balanced ? 'success' : 'danger'">{{ trial.balanced ? '借贷平衡' : '借贷不平！' }}</el-tag>
            <span class="hint" v-if="trial.balanced">有借必有贷、借贷必相等，全部凭证自动校验通过</span>
          </div>
        </div>

        <div class="panel">
          <h3>财务账 ↔ 业务台账对账
            <el-button size="small" style="margin-left: 14px" @click="loadReconcile">刷新</el-button>
            <el-button size="small" style="margin-left: 4px" @click="doBackfill">历史补账</el-button>
          </h3>
          <div class="hint" style="margin-bottom: 10px">
            对账线①：净资产（3001/3002）↔ 业务台账户账合计（期初+交存−分摊）；对账线②：待分配累计收益（3101）↔ 小区公共账（利息）。
            首次使用请先按小区生成「期初建账凭证」。差额为 0 表示两本账一致。
            启用财务账套之前产生的业务凭证不会自动入财务账，点「历史补账」一次性补齐。
          </div>
          <el-table :data="reconcile" size="small" border>
            <el-table-column prop="community" label="小区" min-width="120" />
            <el-table-column label="资金性质" width="90" align="center">
              <template #default="{ row }">{{ row.fundType === 'public' ? '公有住房' : '商品住宅' }}</template>
            </el-table-column>
            <el-table-column label="净资产（财务账）" align="right" :formatter="moneyFmt" prop="glNetAsset" />
            <el-table-column label="户账合计（台账）" align="right" :formatter="moneyFmt" prop="bizHousehold" />
            <el-table-column label="差额①" width="90" align="right">
              <template #default="{ row }">
                <span :style="{ color: Math.abs(row.netDiff) < 0.005 ? '#67c23a' : '#f56c6c' }">{{ fmt(row.netDiff) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="待分配收益（财务账）" align="right" :formatter="moneyFmt" prop="glPending" />
            <el-table-column label="公共账（台账）" align="right" :formatter="moneyFmt" prop="bizPublic" />
            <el-table-column label="差额②" width="90" align="right">
              <template #default="{ row }">
                <span :style="{ color: Math.abs(row.pendingDiff) < 0.005 ? '#67c23a' : '#f56c6c' }">{{ fmt(row.pendingDiff) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="期初建账" width="130" align="center">
              <template #default="{ row }">
                <el-button v-if="!row.openingDone" size="small" type="primary" link @click="genOpening(row)">生成期初凭证</el-button>
                <el-tag v-else size="small" type="success">已建账</el-tag>
              </template>
            </el-table-column>
          </el-table>
          <div class="hint" style="margin-top: 10px">
            注意：差额①在月末结转前会等于「本月未结转的支出」，属于正常现象；月结后差额应为 0。
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { printHTML, printVoucher } from '../print'

const tab = ref('balances')
const moneyFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const today = () => new Date().toISOString().slice(0, 10)

// ---- 科目余额表 ----
const bMonth = ref(today().slice(0, 7))
const rawBalances = ref([])
const subjects = ref([])
const communities = ref([])

const balanceRows = computed(() => {
  // 把带项目辅助核算的二级科目行与一级科目行组装成平铺表格
  const out = []
  const byParent = {}
  for (const r of rawBalances.value) {
    if (r.parent) {
      if (!byParent[r.parent]) byParent[r.parent] = []
      byParent[r.parent].push(r)
    }
  }
  for (const r of rawBalances.value) {
    if (r.parent) continue // 一级科目行后插
    const agg = { debit: 0, credit: 0, balance: 0 }
    for (const c of byParent[r.code] || []) {
      agg.debit += c.debit; agg.credit += c.credit; agg.balance += c.balance
    }
    if (Object.keys(byParent[r.code] || {}).length) {
      out.push({ ...r, name: r.name + '（小计）', project: '', ...agg, key: r.code + '-agg' })
      for (const c of byParent[r.code]) out.push({ ...c, key: c.code + c.project })
    } else {
      out.push({ ...r, key: r.code })
    }
  }
  // 无下级的独立科目（3001/3002 等）
  for (const r of rawBalances.value) {
    if (r.parent === '' && !rawBalances.value.some((x) => x.parent === r.code)) {
      if (!out.some((o) => o.code === r.code)) out.push({ ...r, key: r.code })
    }
  }
  return out.sort((a, b) => a.key.localeCompare(b.key))
})

// ---- 总账（总分类账） ----
const gMonth = ref(today().slice(0, 7))
const gRows = ref([])
function loadGLedger() { api.get('/gl/general-ledger', { params: { month: gMonth.value } }).then((d) => (gRows.value = d.rows || [])) }
function gSummary({ columns, data }) {
  const sums = ['合计', '', '', '', 0, 0, 0, 0]
  for (const r of data) { sums[4] += r.opening; sums[5] += r.debit; sums[6] += r.credit; sums[7] += r.closing }
  return sums.map((v, i) => (i >= 4 ? fmt(v) : v))
}
function printGLedger() {
  const rowsHtml = gRows.value.map((r) => `<tr><td>${r.code}</td><td>${r.name}</td><td>${r.dir === 'debit' ? '借' : '贷'}</td>
    <td class="num">${fmt(r.opening)}</td><td class="num">${fmt(r.debit)}</td><td class="num">${fmt(r.credit)}</td><td class="num">${fmt(r.closing)}</td></tr>`).join('')
  printHTML(`总分类账（${gMonth.value}）`,
    `<table border="1" cellspacing="0" cellpadding="6" style="border-collapse:collapse;width:100%;font-size:12px">
      <tr style="background:#f0f0f0"><th>科目编码</th><th>科目名称</th><th>方向</th><th>期初余额</th><th>本期借方发生</th><th>本期贷方发生</th><th>期末余额</th></tr>${rowsHtml}</table>`)
}

async function doTransfer() {
  const m = gMonth.value
  try {
    await ElMessageBox.confirm(
      `将 ${m} 的收入类科目余额（交存收入、利息收入等）与支出类科目余额结转入净资产，生成一张结转凭证。该操作不锁账，之后仍可继续记账（再次结转只处理新增部分）。是否执行？`,
      `月末结转 ${m}`, { confirmButtonText: '确认结转', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  try {
    const res = await api.post('/gl/transfer', { month: m })
    ElMessage.success(`结转凭证 ${res.no} 已生成并过账`)
    loadGLedger(); loadBalances(); loadGlVouchers(); loadTrial(); loadReconcile()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

// ---- 明细账 ----
const eSubject = ref('')
const eProject = ref('')
const eFrom = ref('')
const eTo = ref('')
const entries = ref([])

// ---- 财务凭证 ----
const glVouchers = ref([])
const vMonth = ref('')
const drawer = ref(false)
const glDetail = ref(null)
const closedMonths = ref([])

// ---- 试算与对账 ----
const trial = ref(null)
const reconcile = ref([])

async function loadBase() {
  subjects.value = await api.get('/gl/subjects')
  communities.value = await api.get('/communities')
  closedMonths.value = (await api.get('/periods')).map((p) => p.month)
}
function loadBalances() { api.get('/gl/balances', { params: { month: bMonth.value } }).then((d) => (rawBalances.value = d)) }
function loadEntries() {
  api.get('/gl/entries', {
    params: { subject: eSubject.value || '', projectId: eProject.value || '', from: eFrom.value || '', to: eTo.value || '' },
  }).then((d) => (entries.value = d))
}
function loadGlVouchers() {
  api.get('/gl/vouchers', { params: { month: vMonth.value || '' } }).then((d) => (glVouchers.value = d))
}
async function showGlVoucher(row) {
  glDetail.value = await api.get(`/gl/vouchers/${row.id}`)
  drawer.value = true
}

function printGlVoucher() {
  const d = glDetail.value
  const subMap = {}
  for (const s of subjects.value) subMap[s.code] = s
  const parentName = (code) => {
    const s = subMap[code]
    if (!s || !s.parent) return s ? s.name : code
    const p = subMap[s.parent]
    return p ? p.name : s.name
  }
  printVoucher({
    no: d.no,
    date: d.date,
    summary: d.summary,
    appendix: 0,
    entries: d.entries.map((e) => ({
      summary: d.summary,
      subject: parentName(e.subject),
      sub: e.subjectName + (e.project ? '（' + e.project + '）' : ''),
      direction: e.direction,
      amount: e.amount,
    })),
  })
}
function loadTrial() { api.get('/gl/trial-balance').then((d) => (trial.value = d)) }
function loadReconcile() { api.get('/gl/reconcile').then((d) => (reconcile.value = d.rows)) }

async function doBackfill() {
  try {
    const res = await api.post('/gl/backfill')
    const skipped = (res.skipped || []).length
    let msg = `已补齐 ${res.backfilled} 张历史凭证的财务记账凭证`
    if (skipped > 0) msg += `，另有 ${skipped} 张因所在月份已月结锁账被跳过（请先反结转再补）`
    ElMessage.success(msg)
    loadReconcile()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function genOpening(row) {
  try {
    const res = await api.post('/gl/opening-balance', { communityId: row.communityId })
    ElMessage.success(`期初建账凭证已生成：¥ ${fmt(res.amount)}`)
    loadReconcile()
    loadGlVouchers()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

// ---- 填制凭证（手工单据） ----
const mDate = ref(today())
const mSummary = ref('')
const mEntries = ref([
  { direction: 'debit', subjectCode: '100101', projectId: null, amount: null },
  { direction: 'credit', subjectCode: '400101', projectId: null, amount: null },
])
const mDebit = computed(() => mEntries.value.reduce((s, r) => s + (r.direction === 'debit' ? Number(r.amount || 0) : 0), 0))
const mCredit = computed(() => mEntries.value.reduce((s, r) => s + (r.direction === 'credit' ? Number(r.amount || 0) : 0), 0))
const mBalanced = computed(() =>
  mEntries.value.length >= 2 &&
  mEntries.value.every((r) => r.subjectCode && Number(r.amount) > 0) &&
  Math.abs(mDebit.value - mCredit.value) < 0.005)

function addEntryRow() {
  mEntries.value.push({ direction: 'debit', subjectCode: '', projectId: null, amount: null })
}

async function saveManual() {
  try {
    const res = await api.post('/gl/manual-voucher', {
      date: mDate.value, summary: mSummary.value,
      entries: mEntries.value.map((r) => ({
        subjectCode: r.subjectCode, projectId: r.projectId || 0,
        direction: r.direction, amount: Number(r.amount),
      })),
    })
    ElMessage.success(`记账凭证 ${res.no} 已保存并过账`)
    mSummary.value = ''
    mEntries.value = [
      { direction: 'debit', subjectCode: '100101', projectId: null, amount: null },
      { direction: 'credit', subjectCode: '400101', projectId: null, amount: null },
    ]
    loadBalances(); loadEntries(); loadGlVouchers(); loadTrial(); loadReconcile()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(() => {
  loadBase()
  loadBalances()
  loadEntries()
  loadGlVouchers()
  loadTrial()
  loadReconcile()
})
</script>

<style scoped>
.hint { color: #909399; font-size: 12px; }
</style>
