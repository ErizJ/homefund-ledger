<template>
  <div>
    <h2 style="margin: 0 0 16px">凭证记账</h2>
    <div class="panel">
      <h3>新增凭证</h3>
      <el-form :model="form" label-width="90px" inline>
        <el-form-item label="凭证类型">
          <el-select v-model="form.type" style="width: 240px" @change="form.buildingId = null; form.householdId = null">
            <el-option label="缴纳收入（记到户）" value="income" />
            <el-option label="维修支出（自动分摊到户）" value="expense" />
            <el-option label="利息收入（挂小区公共账）" value="interest" />
          </el-select>
        </el-form-item>
        <el-form-item label="日期">
          <el-date-picker v-model="form.date" type="date" value-format="YYYY-MM-DD" style="width: 140px" />
        </el-form-item>
        <el-form-item label="小区">
          <el-select v-model="form.communityId" style="width: 180px" placeholder="请选择"
            @change="form.buildingId = null; form.householdId = null; loadBuildings()">
            <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.type !== 'interest'" label="楼洞">
          <el-select v-model="form.buildingId" style="width: 180px" placeholder="请选择"
            @change="form.householdId = null; loadHouseholds()">
            <el-option v-if="form.type === 'expense'" label="全体楼洞（小区级支出）" :value="-1" />
            <el-option v-for="b in buildings" :key="b.id" :label="b.name" :value="b.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.type === 'income'" label="户室">
          <el-select v-model="form.householdId" style="width: 200px" placeholder="请选择" filterable>
            <el-option v-for="h in households" :key="h.id" :label="h.roomNo + (h.owner ? ` (${h.owner})` : '')" :value="h.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="金额（元）">
          <el-input-number v-model="form.amount" :min="0.01" :precision="2" :controls="false" style="width: 140px" placeholder="0.00" />
        </el-form-item>
        <el-form-item v-if="form.type === 'expense'" label="费用类别">
          <el-select v-model="form.category" style="width: 180px">
            <el-option label="工程维修费" value="engineering" />
            <el-option label="监理费" value="supervision" />
            <el-option label="检测费、勘察设计费" value="survey" />
            <el-option label="其他维修相关费用" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="摘要">
          <el-input v-model="form.summary" style="width: 240px" placeholder="如：3栋屋顶防水工程" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="submit">记账</el-button>
        </el-form-item>
      </el-form>
      <div v-if="form.type === 'expense'" class="hint">
        维修支出保存后，将按各户建筑面积占比自动生成「分摊到户」子凭证，尾差由最后一户承担，保证分摊合计 = 支出金额。
      </div>
    </div>

    <div class="panel">
      <h3>凭证列表</h3>
      <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
        <el-date-picker v-model="fMonth" type="month" value-format="YYYY-MM" placeholder="按月份筛选" style="width: 150px" @change="loadList" clearable />
        <el-select v-model="fType" style="width: 150px" placeholder="全部类型" clearable @change="loadList">
          <el-option v-for="(label, key) in TYPE_LABEL" :key="key" :label="label" :value="key" />
        </el-select>
        <el-button size="small" @click="exportVouchers">导出 Excel</el-button>
        <span class="hint">最多显示 500 条</span>
      </div>
      <el-table :data="list" size="small" border>
        <el-table-column prop="no" label="凭证号" width="150" />
        <el-table-column prop="date" label="日期" width="100" />
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="tagType(row.type)" size="small">{{ TYPE_LABEL[row.type] }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="community" label="小区" width="120" />
        <el-table-column label="楼洞" width="100">
          <template #default="{ row }">{{ row.building || '—' }}</template>
        </el-table-column>
        <el-table-column label="户室" width="130">
          <template #default="{ row }">{{ row.roomNo ? row.roomNo + (row.owner ? ' ' + row.owner : '') : '—' }}</template>
        </el-table-column>
        <el-table-column prop="amount" label="金额" align="right" width="120" :formatter="moneyFmt" />
        <el-table-column prop="summary" label="摘要" min-width="180" show-overflow-tooltip />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'voided'" type="info" size="small">已作废</el-tag>
            <span v-else>正常</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="240">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="printVoucher(row)">打印</el-button>
            <el-button size="small" link type="primary" @click="printClassicVoucher(row)">记帐凭证</el-button>
            <el-button v-if="row.type === 'expense'" size="small" link type="primary" @click="printNotices(row)">通知单</el-button>
            <el-button size="small" link type="primary" @click="openAtt(row)">附件</el-button>
            <el-button v-if="row.status === 'normal' && row.type !== 'allocate'" type="danger" size="small" link
              @click="voidVoucher(row)">作废</el-button>
            <span v-else-if="row.type === 'allocate'" class="hint">随主凭证</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="attVisible" title="凭证附件" width="560">
      <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
        <el-button type="primary" size="small" @click="attFileRef.click()">上传票据/照片</el-button>
        <input ref="attFileRef" type="file" style="display: none" @change="onAttFile" />
        <span class="hint">支持图片、PDF 等，单个不超过 20MB</span>
      </div>
      <el-table :data="attList" size="small" border>
        <el-table-column prop="filename" label="文件名" min-width="200" show-overflow-tooltip />
        <el-table-column label="大小" width="90">
          <template #default="{ row }">{{ (row.size / 1024).toFixed(1) }} KB</template>
        </el-table-column>
        <el-table-column label="操作" width="130">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="downloadAtt(row)">下载</el-button>
            <el-button size="small" link type="danger" @click="deleteAtt(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-if="!attList.length" class="hint" style="margin-top: 8px">暂无附件</div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { printHTML, printVoucher as printPaperVoucher, fmtMoney } from '../print'
import { exportExcel } from '../export'

const TYPE_LABEL = { income: '缴纳收入', expense: '维修支出', interest: '利息收入', allocate: '分摊到户' }

// 附件与打印
const attVisible = ref(false)
const attList = ref([])
const attVoucher = ref(null)
const attFileRef = ref(null)

async function openAtt(row) {
  attVoucher.value = row
  const detail = await api.get(`/vouchers/${row.id}`)
  attList.value = detail.attachments || []
  attVisible.value = true
}

async function onAttFile(e) {
  const file = e.target.files[0]
  if (!file || !attVoucher.value) return
  const fd = new FormData()
  fd.append('file', file)
  try {
    await api.post(`/vouchers/${attVoucher.value.id}/attachments`, fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    ElMessage.success('附件上传成功')
    openAtt(attVoucher.value)
  } catch (err) {
    ElMessage.error('上传失败：' + err.message)
  } finally {
    e.target.value = ''
  }
}

function downloadAtt(row) {
  window.open(`/api/attachments/${row.id}/download`, '_blank')
}

async function deleteAtt(row) {
  try {
    await api.delete(`/attachments/${row.id}`)
    ElMessage.success('已删除')
    openAtt(attVoucher.value)
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function printVoucher(row) {
  const d = await api.get(`/vouchers/${row.id}`)
  const title = `记账凭证 ${d.no}`
  let allocHtml = ''
  if (d.type === 'expense' && d.allocations?.length) {
    allocHtml = `
      <h3 style="font-size:14px;margin:16px 0 8px">分摊明细（共 ${d.allocations.length} 户）</h3>
      <table>
        <tr><th>子凭证号</th><th>户号</th><th>户主</th><th class="num">分摊金额</th></tr>
        ${d.allocations.map((a) => `<tr><td>${a.no}</td><td>${a.roomNo}</td><td>${a.owner || ''}</td><td class="num">${fmtMoney(a.amount)}</td></tr>`).join('')}
      </table>`
  }
  printHTML(title, `
    <h1>住房维修基金记账凭证</h1>
    <div class="meta">凭证号：${d.no}　${d.status === 'voided' ? '【已作废】' : ''}</div>
    <table class="v-grid">
      <tr><td width="20%"><b>日期</b></td><td>${d.date}</td>
          <td width="20%"><b>凭证类型</b></td><td>${TYPE_LABEL[d.type]}</td></tr>
      <tr><td><b>小区</b></td><td>${d.community}</td>
          <td><b>楼洞 / 户室</b></td><td>${d.building || '—'} ${d.roomNo || ''} ${d.owner || ''}</td></tr>
      <tr><td><b>金额</b></td><td colspan="3" style="font-size:16px"><b>¥ ${fmtMoney(d.amount)}</b></td></tr>
      <tr><td><b>摘要</b></td><td colspan="3">${d.summary || ''}</td></tr>
    </table>
    ${allocHtml}
    <div class="sign"><span>经办人</span><span>复核人</span><span>负责人</span><span>日期</span></div>
  `)
}

// 经典纸质记帐凭证：定位业务凭证对应的财务记账凭证，按「记帐凭证」规范版式打印
let glVoucherCache = null
let glSubjectsCache = null

async function printClassicVoucher(row) {
  try {
    if (!glVoucherCache) glVoucherCache = await api.get('/gl/vouchers')
    const gv = glVoucherCache.find((g) => g.sourceType === 'voucher' && g.sourceId === row.id)
    if (!gv) {
      return ElMessage.warning('该业务凭证暂无对应财务记账凭证（启用财务账套前的老凭证请先在财务账套点「历史补账」）')
    }
    if (!glSubjectsCache) glSubjectsCache = await api.get('/gl/subjects')
    const d = await api.get(`/gl/vouchers/${gv.id}`)
    const subMap = {}
    for (const s of glSubjectsCache) subMap[s.code] = s
    const parentName = (code) => {
      const s = subMap[code]
      if (!s || !s.parent) return s ? s.name : code
      const p = subMap[s.parent]
      return p ? p.name : s.name
    }
    printPaperVoucher({
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
  } catch (e) {
    ElMessage.error(e.message)
  }
}

// 分户分摊通知单：每户一张，一页一户
async function printNotices(row) {
  const d = await api.get(`/vouchers/${row.id}`)
  if (!d.allocations?.length) {
    ElMessage.warning('该凭证没有分摊明细')
    return
  }
  const pages = d.allocations.map((a, i) => `
    <div style="page-break-after: ${i < d.allocations.length - 1 ? 'always' : 'auto'}; padding-top: 8px">
      <div style="text-align:right;font-size:12px">通知单编号：${a.no}</div>
      <h1>住房维修基金分摊通知单</h1>
      <div class="meta">${d.community}${d.building ? ' / ' + d.building : ''}　打印时间：${new Date().toLocaleString('zh-CN')}</div>
      <p style="font-size:14px">尊敬的 <b>${a.owner || a.roomNo}</b>（户号：${a.roomNo}）业主：</p>
      <p style="font-size:13px;line-height:1.9">
        本小区实施维修项目「<b>${d.summary || '维修工程'}</b>」，发生日期 ${d.date}，工程总金额
        <b>¥ ${fmtMoney(d.amount)}</b>。按照规定，该费用由相关业主按<b>建筑面积占比</b>共同分摊。
        经核算，您户应分摊金额为：
      </p>
      <table class="v-grid">
        <tr><td width="30%"><b>维修项目</b></td><td>${d.summary || ''}</td></tr>
        <tr><td><b>发生日期</b></td><td>${d.date}</td></tr>
        <tr><td><b>工程总金额</b></td><td>¥ ${fmtMoney(d.amount)}</td></tr>
        <tr><td><b>分摊方式</b></td><td>按建筑面积占比分摊（尾差由最后一户承担）</td></tr>
        <tr><td><b>应分摊金额</b></td><td style="font-size:16px"><b>¥ ${fmtMoney(a.amount)}</b></td></tr>
        <tr><td><b>对应凭证号</b></td><td>主凭证 ${d.no} / 分摊子凭证 ${a.no}</td></tr>
      </table>
      <p style="font-size:12px;color:#555">上述金额已从您户住房维修基金账面余额中列支，如有疑问请与代管单位联系核对。</p>
      <div class="sign"><span>业主确认签字</span><span>物业/业委会经办</span><span>住建局（代管）</span><span>日期</span></div>
    </div>`).join('<hr style="border:none;margin:16px 0">')
  printHTML(`分摊通知单 ${d.no}`, pages)
}

async function exportVouchers() {
  const rows = list.value.map((r) => [r.no, r.date, TYPE_LABEL[r.type] || r.type, r.community, r.building || '',
    r.roomNo || '', r.owner || '', r.amount, r.summary || '', r.status === 'voided' ? '已作废' : '正常'])
  exportExcel(`凭证列表 ${fMonth.value || '全部月份'}.xlsx`, '凭证列表',
    ['凭证号', '日期', '类型', '小区', '楼洞', '户号', '户主', '金额', '摘要', '状态'], rows)
}

function today() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const form = ref({ type: 'income', date: today(), communityId: null, buildingId: null, householdId: null, amount: null, summary: '' })
const communities = ref([])
const buildings = ref([])
const households = ref([])
const list = ref([])
const fMonth = ref(today().slice(0, 7))
const fType = ref('')

const moneyFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const tagType = (t) => ({ income: 'danger', expense: 'success', interest: 'primary', allocate: 'warning' }[t] || 'info')

async function loadBase() {
  communities.value = await api.get('/communities')
}
async function loadBuildings() {
  buildings.value = form.value.communityId ? await api.get('/buildings', { params: { communityId: form.value.communityId } }) : []
}
async function loadHouseholds() {
  households.value = form.value.buildingId && form.value.buildingId > 0
    ? await api.get('/households', { params: { buildingId: form.value.buildingId } }) : []
}
async function loadList() {
  list.value = await api.get('/vouchers', { params: { month: fMonth.value || '', type: fType.value || '' } })
}

async function submit() {
  const f = form.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  if (f.type === 'income' && (!f.buildingId || !f.householdId)) return ElMessage.error('缴纳收入必须选择楼洞和户室')
  if (f.type === 'expense' && !f.buildingId) return ElMessage.error('维修支出请选择楼洞，或选「全体楼洞」做小区级支出')
  try {
    const res = await api.post('/vouchers', {
      type: f.type, date: f.date, communityId: f.communityId,
      buildingId: f.buildingId || 0, householdId: f.householdId || 0,
      scope: f.type === 'expense' && f.buildingId === -1 ? 'community' : 'building',
      amount: f.amount, summary: f.summary,
    })
    ElMessage.success(`凭证 ${res.no} 记账成功${f.type === 'expense' ? `，已分摊到 ${res.allocations} 户` : ''}`)
    f.amount = null; f.summary = ''; f.householdId = null
    loadList()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function voidVoucher(row) {
  try {
    await ElMessageBox.confirm(
      row.type === 'expense'
        ? `作废主凭证 ${row.no} 将同时作废其全部分摊子凭证。确定作废吗？`
        : `确定作废凭证 ${row.no} 吗？作废记录会保留留痕，不会删除。`,
      '作废确认', { type: 'warning' }
    )
  } catch { return }
  try {
    const res = await api.post(`/vouchers/${row.id}/void`)
    ElMessage.success(res.voidedChildren ? `已作废主凭证及 ${res.voidedChildren} 张分摊子凭证` : '已作废')
    loadList()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(() => { loadBase(); loadList() })
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
</style>
