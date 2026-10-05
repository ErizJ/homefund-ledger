<template>
  <div>
    <h2 style="margin: 0 0 16px">银行对账</h2>
    <ReloadBanner :failed="loadFailed" @retry="load" />

    <div class="panel">
      <h3>导入银行流水</h3>
      <div style="display: flex; gap: 10px; align-items: center">
        <el-button type="primary" :loading="importing" @click="fileRef.click()">选择 Excel/CSV 流水文件</el-button>
        <input ref="fileRef" type="file" accept=".xlsx,.xls,.csv" style="display: none" @change="onFile" />
        <span class="hint">列头：日期、金额、摘要（支出为负数或正数均可，按金额绝对值匹配）</span>
      </div>
    </div>

    <div class="panel">
      <h3>流水列表</h3>
      <el-radio-group v-model="fStatus" style="margin-bottom: 12px" @change="load">
        <el-radio-button value="unmatched">未匹配（{{ counts.unmatched }}）</el-radio-button>
        <el-radio-button value="matched">已匹配（{{ counts.matched }}）</el-radio-button>
        <el-radio-button value="ignored">已忽略（{{ counts.ignored }}）</el-radio-button>
        <el-radio-button value="">全部</el-radio-button>
      </el-radio-group>
      <el-table :data="list" size="small" border max-height="520">
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column prop="amount" label="金额" align="right" width="130" :formatter="moneyFmt" />
        <el-table-column prop="summary" label="摘要" min-width="200" show-overflow-tooltip />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="{ unmatched: 'warning', matched: 'success', ignored: 'info' }[row.status]">
              {{ { unmatched: '未匹配', matched: '已匹配', ignored: '已忽略' }[row.status] }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="匹配凭证" width="170">
          <template #default="{ row }">{{ row.matchedVoucherId ? '凭证 #' + row.matchedVoucherId : '—' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140">
          <template #default="{ row }">
            <template v-if="row.status === 'unmatched'">
              <el-button type="primary" size="small" link @click="openMatch(row)">对账</el-button>
              <el-button size="small" link @click="ignore(row)">忽略</el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="matchVisible" title="银行对账：选择对应凭证" width="720">
      <div class="hint" style="margin-bottom: 10px">
        银行流水：{{ current?.date }}　金额 {{ fmt(current?.amount) }}　{{ current?.summary }}<br />
        以下为同金额的未匹配凭证（按日期接近排序），请选择与该笔流水对应的凭证：
      </div>
      <el-table :data="candidates" size="small" border max-height="360" v-loading="matchLoading">
        <el-table-column prop="date" label="日期" width="100" />
        <el-table-column prop="no" label="凭证号" width="150" />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">{{ TYPE_LABEL[row.type] }}</template>
        </el-table-column>
        <el-table-column prop="community" label="小区" width="110" />
        <el-table-column label="户室/楼洞" min-width="110">
          <template #default="{ row }">{{ row.roomNo || row.building || '—' }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="金额" align="right" width="110" :formatter="moneyFmt" />
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button type="primary" size="small" link :loading="matchingId === row.id" @click="doMatch(row)">匹配</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!candidates.length && !matchLoading" class="hint" style="margin-top: 10px">
        没有找到同金额的未匹配凭证——这笔流水可能是未达账项或利息，可先「忽略」或在凭证记账中补录后重新对账。
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import * as XLSX from 'xlsx'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'

const TYPE_LABEL = {
  income: '缴纳收入', expense: '维修支出', interest: '利息收入', allocate: '分摊到户',
  refund: '返还/退返', interest_alloc: '收益分配', interest_alloc_child: '收益分配',
  fund_income: '其他收入', cash: '备用金', bond: '国债投资',
}
const fileRef = ref(null)
const list = ref([])
const fStatus = ref('unmatched')
const counts = ref({ unmatched: 0, matched: 0, ignored: 0 })
const matchVisible = ref(false)
const current = ref(null)
const candidates = ref([])
const importing = ref(false)
const matchLoading = ref(false)
const matchingId = ref(0)
const loadFailed = ref(false)

const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const moneyFmt = (row, col, val) => fmt(val)

// Excel 日期序列号（1900 日期系统）→ YYYY-MM-DD
function excelDate(d) {
  const dt = new Date(Math.round((d - 25569) * 86400 * 1000))
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`
}

// 引号感知的 CSV 解析：字段含逗号/换行（引号包裹）时正确切分
function parseCSV(text) {
  if (text.charCodeAt(0) === 0xfeff) text = text.slice(1)
  const rows = []
  let cur = [], field = '', inQuote = false
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (inQuote) {
      if (ch === '"') {
        if (text[i + 1] === '"') { field += '"'; i++ } else inQuote = false
      } else field += ch
    } else if (ch === '"') inQuote = true
    else if (ch === ',') { cur.push(field); field = '' }
    else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++
      cur.push(field); field = ''
      if (cur.some((v) => v !== '')) rows.push(cur)
      cur = []
    } else field += ch
  }
  cur.push(field)
  if (cur.some((v) => v !== '')) rows.push(cur)
  return rows
}

async function load() {
  try {
    list.value = await api.get('/bank/txns', { params: { status: fStatus.value } })
    const all = await api.get('/bank/txns')
    counts.value = {
      unmatched: all.filter((x) => x.status === 'unmatched').length,
      matched: all.filter((x) => x.status === 'matched').length,
      ignored: all.filter((x) => x.status === 'ignored').length,
    }
    loadFailed.value = false
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载流水失败：' + e.message)
  }
}

async function onFile(e) {
  const file = e.target.files[0]
  if (!file) return
  importing.value = true
  try {
    let json = []
    if (/\.(xlsx|xls)$/i.test(file.name)) {
      const buf = await file.arrayBuffer()
      const wb = XLSX.read(buf)
      json = XLSX.utils.sheet_to_json(wb.Sheets[wb.SheetNames[0]], { raw: true, defval: '' })
    } else {
      const buf = new Uint8Array(await file.arrayBuffer())
      let text = new TextDecoder('utf-8').decode(buf)
      if (text.includes('�')) {
        try { text = new TextDecoder('gbk').decode(buf) } catch { /* utf-8 */ }
      }
      // 引号感知解析：摘要含英文逗号（引号包裹）时不会错列
      const rows = parseCSV(text)
      if (rows.length < 2) throw new Error('没有数据行')
      const header = rows[0].map((h) => h.trim())
      const iD = header.findIndex((h) => h.includes('日期'))
      const iA = header.findIndex((h) => h.includes('金额'))
      const iS = header.findIndex((h) => h.includes('摘要'))
      if (iD < 0 || iA < 0) throw new Error('缺少列头：日期/金额')
      json = rows.slice(1).map((r) => ({
        日期: r[iD] ?? '', 金额: parseFloat(r[iA] ?? 0), 摘要: iS >= 0 ? r[iS] ?? '' : '',
      }))
    }
    const rows = json.map((r) => {
      // 日期：Excel 日期单元格是序列号数字，需转换；文本则按原样匹配格式
      let d = r['日期']
      if (typeof d === 'number') d = excelDate(d)
      else d = String(d ?? '').trim()
      const m = d.match(/^(\d{4})[.\-/年](\d{1,2})[.\-/月](\d{1,2})日?$/)
      if (m) d = `${m[1]}-${m[2].padStart(2, '0')}-${m[3].padStart(2, '0')}`
      return { date: d, amount: Math.abs(Number(r['金额'] ?? 0)), summary: String(r['摘要'] ?? '') }
    })
    const res = await api.post('/bank/import', { rows })
    let msg = `导入完成：${res.inserted} 笔`
    if (res.skipped > 0) msg += `，跳过 ${res.skipped} 笔：\n` + res.errors.join('\n')
    if (res.skipped > 0) ElMessage({ type: 'warning', message: msg, duration: 8000, showClose: true })
    else ElMessage.success(msg)
    await load()
  } catch (err) {
    ElMessage.error('导入失败：' + err.message)
  } finally {
    importing.value = false
    e.target.value = ''
  }
}

async function openMatch(row) {
  matchVisible.value = true
  current.value = row
  matchLoading.value = true
  candidates.value = []
  try {
    candidates.value = await api.get('/bank/candidates', {
      params: { date: row.date, amount: Math.abs(row.amount) },
    })
  } catch (e) {
    ElMessage.error('加载候选凭证失败：' + e.message)
    matchVisible.value = false
  } finally {
    matchLoading.value = false
  }
}

async function doMatch(voucher) {
  matchingId.value = voucher.id
  try {
    await api.post(`/bank/txns/${current.value.id}/match`, { voucherId: voucher.id })
    ElMessage.success(`已匹配凭证 ${voucher.no}`)
    matchVisible.value = false
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    matchingId.value = 0
  }
}

async function ignore(row) {
  try {
    await ElMessageBox.confirm(
      `将该笔流水（${row.date} ￥${fmt(row.amount)}）标记为忽略？忽略后本页不可恢复，请确认它不属于任何凭证。`,
      '忽略流水',
      { type: 'warning', confirmButtonText: '确认忽略', cancelButtonText: '取消' },
    )
  } catch { return }
  try {
    await api.post(`/bank/txns/${row.id}/ignore`)
    ElMessage.success('已标记忽略')
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(load)
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
</style>
