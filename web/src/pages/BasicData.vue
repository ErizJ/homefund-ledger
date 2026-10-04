<template>
  <div>
    <h2 style="margin: 0 0 16px">基础数据</h2>

    <div class="panel">
      <h3>Excel 户表导入</h3>
      <div style="display: flex; gap: 10px; margin-bottom: 10px">
        <el-button @click="downloadTemplate">下载导入模板(.xlsx)</el-button>
        <el-button type="primary" @click="fileRef.click()">选择 Excel/CSV 文件导入</el-button>
        <input ref="fileRef" type="file" accept=".xlsx,.xls,.csv" style="display: none" @change="onFile" />
      </div>
      <div class="hint">
        列头必须为：小区、楼洞、户号、户主、建筑面积、期初余额。已存在的户（小区+楼洞+户号相同）会更新户主和面积，不会重复插入。
      </div>
      <el-divider />
      <el-form inline>
        <el-form-item label="手工新建小区">
          <el-input v-model="newCommunity" placeholder="输入小区名称" style="width: 180px" />
        </el-form-item>
        <el-form-item label="资金性质">
          <el-select v-model="newCommunityFundType" style="width: 140px">
            <el-option label="商品住宅" value="commercial" />
            <el-option label="公有住房" value="public" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button @click="addCommunity">创建小区</el-button>
        </el-form-item>
        <el-form-item label="手工新建楼洞">
          <el-select v-model="selectedCommunity" placeholder="先选所属小区" style="width: 160px">
            <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-input v-model="newBuilding" placeholder="输入楼洞名称" style="width: 160px" />
        </el-form-item>
        <el-form-item>
          <el-button @click="addBuilding">创建楼洞</el-button>
        </el-form-item>
      </el-form>
    </div>

    <div class="panel">
      <h3>单位设置（财务报表编制单位）</h3>
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-bottom: 8px">
        <span class="hint">编制单位名称：</span>
        <el-input v-model="orgNameInput" style="width: 320px" placeholder="如：XX市住房保障中心（代管）" />
        <el-button type="primary" @click="saveOrgName">保存</el-button>
      </div>
      <div class="hint">用于资产负债表/收支表/净资产变动表的"编制单位"栏及打印、PDF、Excel 导出，登录后顶栏同步显示。</div>
    </div>


    <div class="panel">
      <h3>会计科目管理（自定义）</h3>
      <div style="margin-bottom: 12px; display: flex; gap: 10px; align-items: center">
        <el-button type="primary" size="small" @click="openSubjectDlg()">＋ 新增科目</el-button>
        <span class="hint">自定义科目自动进入：手工凭证科目选择、明细账、科目余额表、总账及三张财务报表。
          子科目编码 = 上级编码 + 01/02 时，报表自动归入商品住宅/公有住房分栏。</span>
      </div>
      <el-table :data="subjectList" size="small" border max-height="420">
        <el-table-column label="科目编码" width="110">
          <template #default="{ row }">
            <span :style="row.parent ? 'padding-left:16px;color:#606266' : ''">{{ row.code }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="name" label="科目名称" min-width="170" />
        <el-table-column label="类型" width="90" align="center">
          <template #default="{ row }">{{ TYPE_LABEL2[row.type] || row.type }}</template>
        </el-table-column>
        <el-table-column prop="parent" label="上级科目" width="100">
          <template #default="{ row }">{{ row.parent || '—' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click="openSubjectDlg(row)">编辑</el-button>
            <el-button size="small" link :type="row.enabled ? 'warning' : 'success'" @click="toggleSubject(row)">
              {{ row.enabled ? '停用' : '启用' }}
            </el-button>
            <el-button size="small" link type="danger" @click="delSubject(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="subjectDlgVisible" :title="subjectEditing ? '编辑科目' : '新增科目'" width="460">
      <el-form :model="subjectDlg" label-width="90px">
        <el-form-item label="科目编码" required>
          <el-input v-model="subjectDlg.code" placeholder="2-6 位数字，如 6101" />
        </el-form-item>
        <el-form-item label="科目名称" required>
          <el-input v-model="subjectDlg.name" placeholder="如：专项服务收入" />
        </el-form-item>
        <el-form-item label="科目类型" required>
          <el-select v-model="subjectDlg.type" style="width: 100%">
            <el-option label="资产" value="asset" />
            <el-option label="负债" value="liability" />
            <el-option label="净资产" value="net_asset" />
            <el-option label="收入" value="income" />
            <el-option label="支出" value="expense" />
          </el-select>
        </el-form-item>
        <el-form-item label="上级科目">
          <el-select v-model="subjectDlg.parent" clearable placeholder="留空 = 一级科目" style="width: 100%">
            <el-option v-for="s in topSubjects" :key="s.code" :label="s.code + ' ' + s.name" :value="s.code" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="subjectDlgVisible = false">取消</el-button>
        <el-button type="primary" @click="saveSubject">保存</el-button>
      </template>
    </el-dialog>

    <div class="panel">
      <h3>小区设置（首期交存标准 / 公共账期初）</h3>
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-bottom: 8px">
        <el-select v-model="settingCommunity" placeholder="选择小区" style="width: 200px" @change="onSettingCommunity">
          <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <template v-if="settingRow">
          <span class="hint">首期交存标准：</span>
          <el-input-number v-model="settingRow.firstRate" :min="0" :precision="2" :controls="false"
            style="width: 140px" />
          <span class="hint">元/㎡</span>
          <span class="hint" style="margin-left: 16px">公共账期初（启用系统前未分配的利息等）：</span>
          <el-input-number v-model="settingRow.publicOpening" :min="0" :precision="2" :controls="false"
            style="width: 160px" />
          <span class="hint">元</span>
          <el-button type="primary" @click="saveCommunitySetting">保存设置</el-button>
        </template>
      </div>
      <div class="hint">
        首期交存标准用于计算各户「首期交存额 = 标准 × 建筑面积」，分户余额低于其 30% 时在住户管理与分户余额表中标红提示续筹；
        未设置时以建账期初余额近似。公共账期初计入小区公共账可分配余额；若已生成该小区期初建账凭证，保存时会自动按新口径重新生成（须未月结）。
      </div>
    </div>

    <div class="panel">
      <h3>住户台账</h3>
      <div style="margin-bottom: 12px">
        <el-select v-model="filterCommunity" placeholder="全部小区" clearable style="width: 180px" @change="loadHouseholdList">
          <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <span class="hint" style="margin-left: 10px">共 {{ hhList.length }} 户（最多显示 5000 条）</span>
      </div>
      <el-table :data="hhList" size="small" border max-height="480">
        <el-table-column prop="community" label="小区" width="140" />
        <el-table-column prop="building" label="楼洞" width="100" />
        <el-table-column prop="roomNo" label="户号" width="100" />
        <el-table-column prop="owner" label="户主" width="120" />
        <el-table-column prop="area" label="建筑面积㎡" align="right" :formatter="numFmt" />
        <el-table-column prop="openingBalance" label="期初余额" align="right" :formatter="moneyFmt" />
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button type="danger" size="small" link @click="delHousehold(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import * as XLSX from 'xlsx'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'

const communities = ref([])
const hhList = ref([])
const newCommunity = ref('')
const newCommunityFundType = ref('commercial')
const newBuilding = ref('')
const selectedCommunity = ref(null)
const filterCommunity = ref(null)
const fileRef = ref(null)
const settingCommunity = ref(null)
const settingRow = ref(null)
const orgNameInput = ref('')
const subjectList = ref([])
const subjectDlgVisible = ref(false)
const subjectEditing = ref(null)
const subjectDlg = ref({ code: '', name: '', type: 'asset', parent: '' })
const TYPE_LABEL2 = { asset: '资产', liability: '负债', net_asset: '净资产', income: '收入', expense: '支出' }

async function loadOrgName() {
  try {
    const s = await api.get('/settings')
    orgNameInput.value = s.orgName || ''
  } catch { /* 忽略 */ }
}

async function saveOrgName() {
  if (!orgNameInput.value.trim()) return ElMessage.error('编制单位名称不能为空')
  try {
    const res = await api.put('/settings', { orgName: orgNameInput.value.trim() })
    orgNameInput.value = res.orgName
    ElMessage.success('编制单位已保存')
  } catch (e) {
    ElMessage.error(e.message)
  }
}

const moneyFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const numFmt = (row, col, val) => Number(val || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

async function loadBase() {
  communities.value = await api.get('/communities')
}
async function loadHouseholdList() {
  hhList.value = await api.get('/households', {
    params: filterCommunity.value ? { communityId: filterCommunity.value } : {},
  })
}

function downloadTemplate() {
  const data = [
    { 小区: '阳光花园', 楼洞: '1栋', 户号: '101', 户主: '张三', 建筑面积: 89.5, 期初余额: 1200 },
    { 小区: '阳光花园', 楼洞: '1栋', 户号: '102', 户主: '李四', 建筑面积: 105.2, 期初余额: 1400 },
  ]
  const ws = XLSX.utils.json_to_sheet(data)
  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, ws, '户表')
  XLSX.writeFile(wb, '维修基金户表模板.xlsx')
}

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

async function onFile(e) {
  const file = e.target.files[0]
  if (!file) return
  try {
    let json = []
    if (/\.(xlsx|xls)$/i.test(file.name)) {
      const buf = await file.arrayBuffer()
      const wb = XLSX.read(buf)
      json = XLSX.utils.sheet_to_json(wb.Sheets[wb.SheetNames[0]])
    } else {
      const buf = new Uint8Array(await file.arrayBuffer())
      let text = new TextDecoder('utf-8').decode(buf)
      if (text.includes('\uFFFD')) {
        try { text = new TextDecoder('gbk').decode(buf) } catch { /* 保持 utf-8 */ }
      }
      const rows = parseCSV(text)
      if (rows.length < 2) throw new Error('表格没有数据行')
      const header = rows[0].map((h) => h.trim())
      const idx = (names) => header.findIndex((h) => names.some((n) => h.includes(n)))
      const iC = idx(['小区']), iB = idx(['楼洞']), iR = idx(['户号', '室号'])
      const iO = idx(['户主']), iA = idx(['面积']), iOp = idx(['期初'])
      if (iC < 0 || iB < 0 || iR < 0) throw new Error('CSV 缺少必需列头：小区/楼洞/户号')
      json = rows.slice(1).map((r) => ({
        小区: r[iC], 楼洞: r[iB], 户号: r[iR], 户主: iO >= 0 ? r[iO] : '',
        建筑面积: iA >= 0 ? parseFloat(r[iA]) || 0 : 0, 期初余额: iOp >= 0 ? parseFloat(r[iOp]) || 0 : 0,
      }))
    }
    if (!json.length) throw new Error('表格没有数据行')
    const payload = json.map((r) => ({
      community: String(r['小区'] ?? '').trim(),
      building: String(r['楼洞'] ?? '').trim(),
      room: String(r['户号'] ?? '').trim(),
      owner: String(r['户主'] ?? '').trim(),
      area: Number(r['建筑面积'] ?? 0),
      opening: Number(r['期初余额'] ?? 0),
    }))
    const res = await api.post('/households/import', { rows: payload })
    let msg = `导入完成：新增 ${res.inserted} 户，更新 ${res.updated} 户`
    if (res.skipped > 0) msg += `，跳过 ${res.skipped} 行问题数据：\n` + res.errors.join('\n')
    if (res.skipped > 0) ElMessage({ type: 'warning', message: msg, duration: 8000, showClose: true })
    else ElMessage.success(msg)
    await loadBase(); await loadHouseholdList()
  } catch (err) {
    ElMessage.error('导入失败：' + err.message)
  } finally {
    e.target.value = ''
  }
}

function onSettingCommunity() {
  const c = communities.value.find((x) => x.id === settingCommunity.value)
  settingRow.value = c ? { firstRate: Number(c.firstRate || 0), publicOpening: Number(c.publicOpening || 0) } : null
}

async function saveCommunitySetting() {
  const c = communities.value.find((x) => x.id === settingCommunity.value)
  if (!c || !settingRow.value) return
  try {
    await api.put(`/communities/${c.id}`, {
      firstRate: Number(settingRow.value.firstRate || 0),
      publicOpening: Number(settingRow.value.publicOpening || 0),
    })
    ElMessage.success(`「${c.name}」设置已保存`)
    await loadBase()
    onSettingCommunity()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function addCommunity() {
  const name = newCommunity.value.trim()
  if (!name) return
  try {
    await api.post('/communities', { name, fundType: newCommunityFundType.value })
    ElMessage.success(`小区「${name}」创建成功`)
    newCommunity.value = ''
    await loadBase()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function addBuilding() {
  const name = newBuilding.value.trim()
  if (!selectedCommunity.value || !name) return ElMessage.warning('请先选择所属小区并输入楼洞名称')
  try {
    await api.post('/buildings', { communityId: selectedCommunity.value, name })
    ElMessage.success(`楼洞「${name}」创建成功`)
    newBuilding.value = ''
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function delHousehold(row) {
  try {
    await ElMessageBox.confirm(`确认删除住户 ${row.roomNo}（${row.owner}）？`, '删除确认', { type: 'warning' })
  } catch { return }
  try {
    await api.delete(`/households/${row.id}`)
    ElMessage.success('已删除')
    loadHouseholdList()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function loadSubjects() {
  subjectList.value = await api.get('/gl/subjects')
}
const topSubjects = computed(() => subjectList.value.filter((x) => !x.parent))

function openSubjectDlg(row) {
  if (row) {
    subjectEditing.value = row
    subjectDlg.value = { code: row.code, name: row.name, type: row.type, parent: row.parent || '' }
  } else {
    subjectEditing.value = null
    subjectDlg.value = { code: '', name: '', type: 'asset', parent: '' }
  }
  subjectDlgVisible.value = true
}

async function saveSubject() {
  const d = subjectDlg.value
  if (!d.code || !d.name) return ElMessage.error('科目编码和名称不能为空')
  try {
    if (subjectEditing.value) {
      const row = subjectEditing.value
      await api.put(`/gl/subjects/${row.code}`, { name: d.name, code: d.code, type: d.type, parent: d.parent || '' })
      ElMessage.success(`科目 ${d.code} 已更新`)
    } else {
      await api.post('/gl/subjects', { code: d.code, name: d.name, type: d.type, parent: d.parent || '' })
      ElMessage.success(`科目 ${d.code} 已新增（报表将自动包含该科目行）`)
    }
    subjectDlgVisible.value = false
    loadSubjects()
  } catch (e) { ElMessage.error('保存科目失败：' + e.message) }
}

async function toggleSubject(row) {
  try {
    await api.put(`/gl/subjects/${row.code}`, { enabled: !row.enabled })
    ElMessage.success(row.enabled ? `科目 ${row.code} 已停用` : `科目 ${row.code} 已启用`)
    loadSubjects()
  } catch (e) { ElMessage.error(e.message) }
}

async function delSubject(row) {
  try {
    await ElMessageBox.confirm(
      `确认删除科目 ${row.code} ${row.name}？已有分录的科目不能删除（请改为停用）。`, '删除科目', { type: 'warning' })
  } catch { return }
  try {
    await api.delete(`/gl/subjects/${row.code}`)
    ElMessage.success(`科目 ${row.code} 已删除`)
    loadSubjects()
  } catch (e) { ElMessage.error('删除失败：' + e.message) }
}

onMounted(async () => { await loadBase(); await loadHouseholdList(); await loadOrgName(); await loadSubjects() })
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
</style>
