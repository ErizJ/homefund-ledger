<template>
  <div>
    <h2 style="margin: 0 0 16px">账簿查询（财会〔2020〕7号 · 收付实现制 · 借贷记账法）</h2>
    <ReloadBanner :failed="loadFailed" @retry="init" />

    <el-tabs v-model="tab">
      <!-- ==================== 总账（总分类账） ==================== -->
      <el-tab-pane label="总账" name="generalLedger">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
          <el-date-picker v-model="gMonth" type="month" value-format="YYYY-MM" style="width: 150px"
            :clearable="false" @change="loadGLedger" />
          <el-button size="small" @click="printGLedger">打印总账</el-button>
          <span class="hint">三栏式总账：记账凭证保存后自动过账计入，收入/支出类科目月末结转时并入净资产</span>
        </div>
        <el-table :data="gRows" size="default" border :show-summary="gRows.length > 0" :summary-method="gSummary">
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
          月末结转在「期末业务」页执行：结转后收入类科目余额清零、并入净资产（3001/3002），与业务台账对账线保持勾稽。
        </div>
      </el-tab-pane>

      <!-- ==================== 明细账 ==================== -->
      <el-tab-pane label="明细账" name="entries">
        <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap">
          <el-radio-group v-model="eMode" @change="loadEntries">
            <el-radio-button value="group">按小区汇总</el-radio-button>
            <el-radio-button value="detail">逐笔明细</el-radio-button>
          </el-radio-group>
          <el-select v-model="eSubject" placeholder="选择科目（可输编码过滤）" filterable style="width: 280px" clearable @change="loadEntries">
            <el-option v-for="s in subjects" :key="s.code"
              :label="(s.parent ? '　　' : '') + s.code + ' ' + s.name" :value="s.code" />
          </el-select>
          <el-select v-model="eProject" placeholder="全部小区" style="width: 160px" clearable @change="loadEntries">
            <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
          </el-select>
          <el-date-picker v-model="eFrom" type="date" value-format="YYYY-MM-DD" placeholder="开始日期" style="width: 140px" @change="loadEntries" />
          <el-date-picker v-model="eTo" type="date" value-format="YYYY-MM-DD" placeholder="结束日期" style="width: 140px" @change="loadEntries" />
        </div>
        <!-- 按小区汇总 -->
        <el-table v-if="eMode === 'group'" :data="entries" size="small" border :show-summary="entries.length > 0" :summary-method="groupSummary">
          <el-table-column prop="subject" label="科目" width="90" />
          <el-table-column prop="subjectName" label="科目名称" min-width="170" />
          <el-table-column prop="project" label="小区" min-width="140" />
          <el-table-column label="借方发生合计" align="right" :formatter="moneyFmt" prop="debit" />
          <el-table-column label="贷方发生合计" align="right" :formatter="moneyFmt" prop="credit" />
          <el-table-column label="净发生额（借+贷−）" align="right" :formatter="moneyFmt" prop="net" />
          <el-table-column prop="count" label="笔数" width="80" align="right" />
        </el-table>
        <!-- 逐笔明细 -->
        <el-table v-else :data="entries" size="small" border>
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

      <!-- ==================== 财务报表（财会〔2020〕7号附录，分栏式） ==================== -->
      <el-tab-pane label="财务报表" name="statements">
        <div style="margin-bottom: 14px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap">
          <el-radio-group v-model="stMode" @change="loadStatements">
            <el-radio-button value="month">月度</el-radio-button>
            <el-radio-button value="year">年度</el-radio-button>
          </el-radio-group>
          <el-date-picker v-if="stMode === 'month'" v-model="stMonth" type="month" value-format="YYYY-MM"
            style="width: 150px" :clearable="false" @change="loadStatements" />
          <el-date-picker v-else v-model="stYear" type="year" value-format="YYYY"
            style="width: 120px" :clearable="false" @change="loadStatements" />
          <span class="hint">分栏口径：商品住宅 / 已售公有住房（按科目资金性质分账）　编制单位：{{ bs ? bs.org : '—' }}</span>
        </div>

        <!-- 诊断引导 -->
        <div v-if="bs" style="margin-bottom: 14px">
          <el-alert v-for="d in bs.diagnostics" :key="d.type" :type="diagType(d.level)" :title="d.message"
            show-icon :closable="false" style="margin-bottom: 6px" />
        </div>

        <!-- 会住维01表 资产负债表 -->
        <div class="panel">
          <h3>资产负债表（会住维01表）
            <el-button size="small" style="margin-left: 14px" @click="previewBalanceSheet">预览打印</el-button>
            <el-button size="small" @click="downloadBS_PDF">下载 PDF</el-button>
            <el-button size="small" @click="exportBalanceSheet">导出 Excel</el-button>
            <el-tag v-if="bs" size="small" :type="bs.balanced ? 'success' : 'danger'" style="margin-left: 8px">
              {{ bs.balanced ? '资产 = 负债和净资产' : '不平衡（见上方诊断）' }}
            </el-tag>
          </h3>
          <el-table :data="bsView" size="small" border v-if="bs">
            <el-table-column label="项目" min-width="170">
              <template #default="{ row }">
                <span :style="row.isSection || row.isTotal ? 'font-weight:600' : ''">{{ row.name }}</span>
              </template>
            </el-table-column>
            <el-table-column label="商品住宅 年初" align="right" width="118">
              <template #default="{ row }">{{ row.openComm }}</template>
            </el-table-column>
            <el-table-column label="公有住房 年初" align="right" width="118">
              <template #default="{ row }">{{ row.openPub }}</template>
            </el-table-column>
            <el-table-column label="合计 年初" align="right" width="118">
              <template #default="{ row }">{{ row.openTotal }}</template>
            </el-table-column>
            <el-table-column label="商品住宅 期末" align="right" width="118">
              <template #default="{ row }">{{ row.closComm }}</template>
            </el-table-column>
            <el-table-column label="公有住房 期末" align="right" width="118">
              <template #default="{ row }">{{ row.closPub }}</template>
            </el-table-column>
            <el-table-column label="合计 期末" align="right" width="118">
              <template #default="{ row }">{{ row.closTotal }}</template>
            </el-table-column>
          </el-table>
          <div v-if="!bs" class="hint">加载中…</div>
        </div>

        <!-- 会住维02表 收支表 -->
        <div class="panel">
          <h3>收支表（会住维02表）
            <el-button size="small" style="margin-left: 14px" @click="previewIncomeStatement">预览打印</el-button>
            <el-button size="small" @click="downloadIS_PDF">下载 PDF</el-button>
            <el-button size="small" @click="exportIncomeStatement">导出 Excel</el-button>
          </h3>
          <el-table :data="isView" size="small" border v-if="incomeStmt">
            <el-table-column label="项目" min-width="170">
              <template #default="{ row }">
                <span :style="row.isSection || row.isTotal ? 'font-weight:600' : ''">{{ row.label }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="'商品住宅 ' + isCurLabel" align="right" width="118">
              <template #default="{ row }">{{ row.curComm }}</template>
            </el-table-column>
            <el-table-column :label="'公有住房 ' + isCurLabel" align="right" width="118">
              <template #default="{ row }">{{ row.curPub }}</template>
            </el-table-column>
            <el-table-column :label="'合计 ' + isCurLabel" align="right" width="118">
              <template #default="{ row }">{{ row.curTotal }}</template>
            </el-table-column>
            <el-table-column :label="'商品住宅 ' + isCumLabel" align="right" width="118">
              <template #default="{ row }">{{ row.cumComm }}</template>
            </el-table-column>
            <el-table-column :label="'公有住房 ' + isCumLabel" align="right" width="118">
              <template #default="{ row }">{{ row.cumPub }}</template>
            </el-table-column>
            <el-table-column :label="'合计 ' + isCumLabel" align="right" width="118">
              <template #default="{ row }">{{ row.cumTotal }}</template>
            </el-table-column>
          </el-table>
          <div v-if="!incomeStmt" class="hint">加载中…</div>
        </div>

        <!-- 会住维03表 净资产变动表 -->
        <div class="panel">
          <h3>净资产变动表（会住维03表，年度口径）
            <el-button size="small" style="margin-left: 14px" @click="previewNAS">预览打印</el-button>
            <el-button size="small" @click="downloadNAS_PDF">下载 PDF</el-button>
            <el-button size="small" @click="exportNAS">导出 Excel</el-button>
          </h3>
          <el-alert v-for="d in (nas ? nas.diagnostics || [] : [])" :key="d.type" :type="diagType(d.level)"
            :title="d.message" show-icon :closable="false" style="margin-bottom: 6px" />
          <el-table :data="nas ? nas.rows : []" size="small" border v-if="nas">
            <el-table-column prop="Label" label="项目" min-width="220" />
            <el-table-column v-for="(c, i) in nas.cols" :key="c" :label="c" align="right" width="150">
              <template #default="{ row }">{{ row.Cols[i] }}</template>
            </el-table-column>
          </el-table>
          <div v-if="!nas" class="hint">加载中…</div>
        </div>

        <!-- 打印预览 -->
        <el-dialog v-model="previewVisible" :title="previewTitle" width="92%" top="2vh" destroy-on-close>
          <iframe ref="previewFrame" :srcdoc="previewHtml" class="preview-frame"></iframe>
          <template #footer>
            <el-button @click="previewVisible = false">关闭</el-button>
            <el-button type="primary" @click="doPrintPreview">打 印</el-button>
            <span class="hint" style="margin-right: 8px">打印对话框中选择"另存为 PDF"即可存档电子版</span>
          </template>
        </el-dialog>
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
            <el-button size="small" style="margin-left: 4px" :loading="backfillBusy" @click="doBackfill">历史补账</el-button>
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
                <el-button v-if="!row.openingDone" size="small" type="primary" link
                  :loading="openingBusyId === row.communityId" @click="genOpening(row)">生成期初凭证</el-button>
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
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'
import { printHTML } from '../print'
import { exportExcel } from '../export'
import { nav } from '../store'

const tab = ref(nav.glTab || 'generalLedger')
// 菜单点击账簿子项时同步切换页签
watch(() => nav.glTab, (t) => { tab.value = t })
const moneyFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const today = () => new Date().toISOString().slice(0, 10)

const subjects = ref([])
const communities = ref([])

// ---- 总账（总分类账） ----
const loadFailed = ref(false)
const failLoad = (label) => (e) => {
  loadFailed.value = true
  ElMessage.error(label + '：' + e.message)
}

const gMonth = ref(today().slice(0, 7))
const gRows = ref([])
function loadGLedger() {
  return api.get('/gl/general-ledger', { params: { month: gMonth.value } })
    .then((d) => (gRows.value = d.rows || []))
    .catch(failLoad('加载总账失败'))
}
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

// ---- 明细账 ----
const eMode = ref('group') // group=按小区汇总；detail=逐笔明细
const eSubject = ref('')
const eProject = ref('')
const eFrom = ref('')
const eTo = ref('')
const entries = ref([])
function loadEntries() {
  return api.get('/gl/entries', {
    params: { subject: eSubject.value || '', projectId: eProject.value || '', from: eFrom.value || '', to: eTo.value || '',
      detail: eMode.value === 'detail' ? '1' : '' },
  }).then((d) => (entries.value = d))
    .catch(failLoad('加载明细账失败'))
}
function groupSummary({ columns, data }) {
  const sums = ['合计', '', '', 0, 0, 0, 0]
  for (const r of data) { sums[3] += r.debit; sums[4] += r.credit; sums[5] += r.net; sums[6] += r.count }
  return sums.map((v, i) => (i >= 3 ? fmt(v) : v))
}

// ---- 科目余额表 ----
const bMonth = ref(today().slice(0, 7))
const rawBalances = ref([])
const balanceRows = computed(() => {
  const out = []
  const byParent = {}
  for (const r of rawBalances.value) {
    if (r.parent) {
      if (!byParent[r.parent]) byParent[r.parent] = []
      byParent[r.parent].push(r)
    }
  }
  for (const r of rawBalances.value) {
    if (r.parent) continue
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
  for (const r of rawBalances.value) {
    if (r.parent === '' && !rawBalances.value.some((x) => x.parent === r.code)) {
      if (!out.some((o) => o.code === r.code)) out.push({ ...r, key: r.code })
    }
  }
  return out.sort((a, b) => a.key.localeCompare(b.key))
})
function loadBalances() {
  return api.get('/gl/balances', { params: { month: bMonth.value } })
    .then((d) => (rawBalances.value = d))
    .catch(failLoad('加载科目余额表失败'))
}

// ---- 试算与对账 ----
const trial = ref(null)
const reconcile = ref([])
function loadTrial() {
  return api.get('/gl/trial-balance').then((d) => (trial.value = d)).catch(failLoad('加载试算平衡失败'))
}
function loadReconcile() {
  return api.get('/gl/reconcile').then((d) => (reconcile.value = d.rows)).catch(failLoad('加载对账失败'))
}

const backfillBusy = ref(false)
const openingBusyId = ref(0)

async function doBackfill() {
  backfillBusy.value = true
  try {
    const res = await api.post('/gl/backfill')
    const skipped = (res.skipped || []).length
    let msg = `已补齐 ${res.backfilled} 张历史凭证的财务记账凭证`
    if (skipped > 0) msg += `，另有 ${skipped} 张因所在月份已月结锁账被跳过（请先反结转再补）`
    ElMessage.success(msg)
    await loadReconcile()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    backfillBusy.value = false
  }
}

async function genOpening(row) {
  openingBusyId.value = row.communityId
  try {
    const res = await api.post('/gl/opening-balance', { communityId: row.communityId })
    ElMessage.success(`期初建账凭证已生成：¥ ${fmt(res.amount)}`)
    await loadReconcile()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    openingBusyId.value = 0
  }
}

async function loadBase() {
  try {
    subjects.value = await api.get('/gl/subjects')
    communities.value = await api.get('/communities')
  } catch (e) { failLoad('加载基础数据失败')(e) }
}

// ---- 财务报表（会住维01/02/03表，分栏式） ----
const stMode = ref('month')
const stMonth = ref(today().slice(0, 7))
const stYear = ref(String(new Date().getFullYear()))
const bs = ref(null)
const incomeStmt = ref(null)
const nas = ref(null)
const previewVisible = ref(false)
const previewTitle = ref('')
const previewHtml = ref('')
const previewFrame = ref(null)

const isCurLabel = computed(() => (incomeStmt.value ? incomeStmt.value.curLabel : '本月数'))
const isCumLabel = computed(() => (incomeStmt.value ? incomeStmt.value.cumLabel : '本年累计数'))
const diagType = (l) => ({ error: 'error', warn: 'warning', info: 'success' }[l] || 'info')

const bsView = computed(() => {
  if (!bs.value) return []
  const d = bs.value
  const row = (name, isSection) => ({ name, isSection, openComm: '', openPub: '', openTotal: '', closComm: '', closPub: '', closTotal: '' })
  const total = (label, t) => ({ ...row(label, false), isTotal: true, openComm: t.comm, openPub: t.pub, openTotal: t.total, closComm: t.comm, closPub: t.pub, closTotal: t.total })
  return [
    row('资　产', true),
    ...d.assets.map((r) => ({ name: r.name, openComm: r.openComm, openPub: r.openPub, openTotal: r.openTotal, closComm: r.closComm, closPub: r.closPub, closTotal: r.closTotal })),
    total('资产总计', d.assetsTotalClosing),
    row('负债和净资产', true),
    ...d.equity.map((r) => ({ name: r.name, openComm: r.openComm, openPub: r.openPub, openTotal: r.openTotal, closComm: r.closComm, closPub: r.closPub, closTotal: r.closTotal })),
    total('负债和净资产总计', d.equityTotalClosing),
  ]
})

const isView = computed(() => {
  if (!incomeStmt.value) return []
  const d = incomeStmt.value
  const sec = (label) => ({ label, isSection: true, curComm: '', curPub: '', curTotal: '', cumComm: '', cumPub: '', cumTotal: '' })
  const tot = (label, cur, cum) => ({ label, isTotal: true, curComm: cur.comm, curPub: cur.pub, curTotal: cur.total, cumComm: cum.comm, cumPub: cum.pub, cumTotal: cum.total })
  return [
    sec('一、本期收入'),
    ...d.income.map((r) => ({ label: '　' + r.name, curComm: r.curComm, curPub: r.curPub, curTotal: r.curTotal, cumComm: r.cumComm, cumPub: r.cumPub, cumTotal: r.cumTotal })),
    tot('　收入合计', d.incomeTotal, d.incomeCumTotal),
    sec('二、本期支出'),
    ...d.expense.map((r) => ({ label: '　' + r.name, curComm: r.curComm, curPub: r.curPub, curTotal: r.curTotal, cumComm: r.cumComm, cumPub: r.cumPub, cumTotal: r.cumTotal })),
    tot('　支出合计', d.expenseTotal, d.expenseCumTotal),
    tot('三、本期收支差额', d.diff, d.diffCumulative),
  ]
})

async function loadStatements() {
  const bsMonth = stMode.value === 'month' ? stMonth.value : stYear.value + '-12'
  // 三张表独立加载，一张失败不影响其余
  const [r1, r2, r3] = await Promise.allSettled([
    api.get('/gl/balance-sheet', { params: { month: bsMonth } }),
    api.get('/gl/income-statement', { params: stMode.value === 'month' ? { month: stMonth.value } : { year: stYear.value } }),
    api.get('/gl/net-asset-statement', { params: { month: bsMonth } }),
  ])
  if (r1.status === 'fulfilled') bs.value = r1.value
  else failLoad('资产负债表加载失败')(r1.reason)
  if (r2.status === 'fulfilled') incomeStmt.value = r2.value
  else failLoad('收支表加载失败')(r2.reason)
  if (r3.status === 'fulfilled') nas.value = r3.value
  else failLoad('净资产变动表加载失败')(r3.reason)
}

function stPeriod() {
  return stMode.value === 'month' ? bs.value.asOf : stYear.value + '-12-31'
}
function stPeriodTitle() {
  return stMode.value === 'month' ? stMonth.value : stYear.value + ' 年度'
}

// 打印预览：在弹窗 iframe 中渲染报表 HTML，可打印或另存 PDF
function showPreview(title, body) {
  previewTitle.value = title
  previewHtml.value = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>
    body { font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; padding: 24px; color: #111; }
    h1 { font-size: 20px; text-align: center; margin: 0 0 4px; }
    .meta { text-align: center; font-size: 12px; color: #333; margin-bottom: 14px; }
    table { border-collapse: collapse; width: 100%; font-size: 12px; }
    th, td { border: 1px solid #555; padding: 5px 8px; }
    th { background: #f0f0f0; white-space: pre-line; }
    .num { text-align: right; font-variant-numeric: tabular-nums; }
    .sign { display: flex; justify-content: space-between; margin-top: 28px; font-size: 12px; }
    .sign span { border-top: 1px solid #333; padding: 4px 14px 0; }
    @media print { body { padding: 0; } }
  </style></head><body>${body}</body></html>`
  previewVisible.value = true
}

function doPrintPreview() {
  const f = previewFrame.value
  if (f && f.contentWindow) f.contentWindow.print()
}

function printHeader(title, tableNo, period) {
  return `<h1>${title}</h1>
    <div class="meta">资金名称：住宅专项维修资金　${tableNo}　单位：元</div>
    <div class="meta">编制单位：${bs.value?.org || ''}　${period}　打印时间：${new Date().toLocaleString('zh-CN')}</div>`
}

const signLine = () => '<div class="sign"><span>制表人</span><span>复核人</span><span>负责人</span><span>日期</span></div>'

function previewBalanceSheet() {
  const d = bs.value
  if (!d) return
  const rowsHtml = bsView.value.map((r) => `<tr${r.isTotal ? ' style="font-weight:600"' : ''}${r.isSection ? ' style="font-weight:600;background:#f0f0f0"' : ''}>
    <td>${r.name}</td><td class="num">${r.openComm}</td><td class="num">${r.openPub}</td><td class="num">${r.openTotal}</td>
    <td class="num">${r.closComm}</td><td class="num">${r.closPub}</td><td class="num">${r.closTotal}</td></tr>`).join('')
  showPreview(`资产负债表（${stPeriodTitle()}）`, `${printHeader('住宅专项维修资金资产负债表', '会住维01表', stPeriod())}
    <table><tr><th>项目</th><th class="num">商品住宅\n年初余额</th><th class="num">公有住房\n年初余额</th><th class="num">合计\n年初余额</th>
    <th class="num">商品住宅\n期末余额</th><th class="num">公有住房\n期末余额</th><th class="num">合计\n期末余额</th></tr>${rowsHtml}</table>${signLine()}`)
}

function previewIncomeStatement() {
  const d = incomeStmt.value
  if (!d) return
  const rowsHtml = isView.value.map((r) => `<tr${r.isTotal ? ' style="font-weight:600"' : ''}${r.isSection ? ' style="font-weight:600;background:#f0f0f0"' : ''}>
    <td>${r.label}</td><td class="num">${r.curComm}</td><td class="num">${r.curPub}</td><td class="num">${r.curTotal}</td>
    <td class="num">${r.cumComm}</td><td class="num">${r.cumPub}</td><td class="num">${r.cumTotal}</td></tr>`).join('')
  showPreview(`收支表（${d.title}）`, `${printHeader('住宅专项维修资金收支表', '会住维02表', d.title)}
    <table><tr><th>项目</th><th class="num">商品住宅\n${d.curLabel}</th><th class="num">公有住房\n${d.curLabel}</th><th class="num">合计\n${d.curLabel}</th>
    <th class="num">商品住宅\n${d.cumLabel}</th><th class="num">公有住房\n${d.cumLabel}</th><th class="num">合计\n${d.cumLabel}</th></tr>${rowsHtml}</table>${signLine()}`)
}

function previewNAS() {
  const d = nas.value
  if (!d) return
  const rowsHtml = d.rows.map((r, i) => `<tr${i === d.rows.length - 1 ? ' style="font-weight:600"' : ''}>
    <td>${r.Label}</td>${r.Cols.map((v) => `<td class="num">${v}</td>`).join('')}</tr>`).join('')
  showPreview(`净资产变动表（${d.title}）`, `${printHeader('住宅专项维修资金净资产变动表', '会住维03表', d.title)}
    <table><tr><th>项目</th>${d.cols.map((c) => `<th class="num">${c}</th>`).join('')}</tr>${rowsHtml}</table>${signLine()}`)
}

function downloadBS_PDF() {
  window.open(`/api/gl/balance-sheet/pdf?month=${stMode.value === 'month' ? stMonth.value : stYear.value + '-12'}`, '_blank')
}
function downloadIS_PDF() {
  const q = stMode.value === 'month' ? `month=${stMonth.value}` : `year=${stYear.value}`
  window.open(`/api/gl/income-statement/pdf?${q}`, '_blank')
}
function downloadNAS_PDF() {
  window.open(`/api/gl/net-asset-statement/pdf?month=${stMode.value === 'month' ? stMonth.value : stYear.value + '-12'}`, '_blank')
}

function exportBalanceSheet() {
  const d = bs.value
  if (!d) return
  const rows = [
    ['住宅专项维修资金资产负债表', '', '', '会住维01表', '单位：元'],
    ['编制单位：' + (d.org || ''), stPeriod(), '', '', ''],
    ['项目', '商品住宅\n年初余额', '公有住房\n年初余额', '合计\n年初余额', '商品住宅\n期末余额', '公有住房\n期末余额', '合计\n期末余额'],
    ...bsView.value.map((r) => [r.name, r.openComm, r.openPub, r.openTotal, r.closComm, r.closPub, r.closTotal]),
  ]
  exportExcel(`资产负债表-${stPeriodTitle()}.xlsx`, '会住维01表',
    ['项目', '商品住宅年初余额', '公有住房年初余额', '合计年初余额', '商品住宅期末余额', '公有住房期末余额', '合计期末余额'], rows)
}

function exportIncomeStatement() {
  const d = incomeStmt.value
  if (!d) return
  const rows = [
    ['住宅专项维修资金收支表', '', '', '会住维02表', '单位：元'],
    ['编制单位：' + (d.org || ''), d.title, '', '', ''],
    ['项目', '商品住宅' + d.curLabel, '公有住房' + d.curLabel, '合计' + d.curLabel, '商品住宅' + d.cumLabel, '公有住房' + d.cumLabel, '合计' + d.cumLabel],
    ...isView.value.map((r) => [r.label, r.curComm, r.curPub, r.curTotal, r.cumComm, r.cumPub, r.cumTotal]),
  ]
  exportExcel(`收支表-${d.title}.xlsx`, '会住维02表',
    ['项目', '商品住宅' + d.curLabel, '公有住房' + d.curLabel, '合计' + d.curLabel, '商品住宅' + d.cumLabel, '公有住房' + d.cumLabel, '合计' + d.cumLabel], rows)
}

function exportNAS() {
  const d = nas.value
  if (!d) return
  const rows = [
    ['住宅专项维修资金净资产变动表', '', '会住维03表', '单位：元'],
    ['编制单位：' + (d.org || ''), d.title, '', ''],
    ['项目', ...d.cols],
    ...d.rows.map((r) => [r.Label, ...r.Cols]),
  ]
  exportExcel(`净资产变动表-${d.title}.xlsx`, '会住维03表', ['项目', ...d.cols], rows)
}

async function init() {
  loadFailed.value = false
  await Promise.all([
    loadBase(), loadGLedger(), loadEntries(), loadBalances(),
    loadTrial(), loadReconcile(), loadStatements(),
  ])
}

onMounted(init)
</script>

<style scoped>
.hint { color: #909399; font-size: 12px; }
.ind { padding-left: 16px; color: #606266; }
.preview-frame { width: 100%; height: 66vh; border: 1px solid #e2e5ea; border-radius: 6px; }
</style>
