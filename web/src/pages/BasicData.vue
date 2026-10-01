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
import { ref, onMounted } from 'vue'
import * as XLSX from 'xlsx'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'

const communities = ref([])
const hhList = ref([])
const newCommunity = ref('')
const newBuilding = ref('')
const selectedCommunity = ref(null)
const filterCommunity = ref(null)
const fileRef = ref(null)

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

onMounted(async () => { await loadBase(); await loadHouseholdList() })
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
</style>
