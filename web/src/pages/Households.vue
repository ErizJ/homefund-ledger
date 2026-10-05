<template>
  <div>
    <h2 style="margin: 0 0 16px">住户管理</h2>
    <ReloadBanner :failed="loadFailed" @retry="init" />

    <div class="panel">
      <div style="display: flex; gap: 12px; align-items: center; flex-wrap: wrap; margin-bottom: 14px">
        <el-button type="primary" size="large" @click="openCreate">＋ 新增住户</el-button>
        <el-input v-model="keyword" placeholder="搜索：户主 / 户号 / 小区 / 楼洞" style="width: 280px" size="large"
          clearable @input="onKeywordInput" />
        <el-select v-model="filterCommunity" placeholder="全部小区" clearable style="width: 180px" size="large"
          @change="loadList">
          <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
        </el-select>
        <span class="hint">共 {{ list.length }} 户</span>
        <el-button style="margin-left: auto" @click="goImport">去 Excel 批量导入 →</el-button>
      </div>

      <el-table :data="list" size="default" border max-height="560" highlight-current-row
        @row-click="(row) => openStatement(row)" style="cursor: pointer">
        <el-table-column prop="community" label="小区" min-width="130" />
        <el-table-column prop="building" label="楼洞" width="100" />
        <el-table-column prop="roomNo" label="户号" width="100" />
        <el-table-column prop="owner" label="户主" width="120">
          <template #default="{ row }">{{ row.owner || '—' }}</template>
        </el-table-column>
        <el-table-column prop="area" label="建筑面积" width="110" align="right">
          <template #default="{ row }">{{ Number(row.area).toFixed(2) }} ㎡</template>
        </el-table-column>
        <el-table-column label="当前余额" width="190" align="right">
          <template #default="{ row }">
            <span class="money">¥ {{ fmt(row.balance) }}</span>
            <el-tooltip v-if="row.belowThreshold" placement="top"
              :content="`余额低于首期交存额（¥ ${fmt(row.firstPayment)}）的 30%，按规定应续筹`">
              <el-tag type="danger" size="small" style="margin-left: 6px">低于30%</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <el-button size="small" link type="primary" @click.stop="openStatement(row)">收支流水</el-button>
            <el-button size="small" link type="primary" @click.stop="openEdit(row)">编辑</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="hint" style="margin-top: 8px">点击任意一行可查看该住户的收支流水与余额变化</div>
    </div>

    <!-- 新增/编辑对话框 -->
    <el-dialog v-model="dlgVisible" :title="editingId ? '编辑住户' : '新增住户'" width="460">
      <el-form :model="dlg" label-width="100px">
        <template v-if="!editingId">
          <el-form-item label="小区" required>
            <el-select v-model="dlg.communityId" placeholder="请选择" style="width: 100%"
              @change="dlg.buildingId = null; loadDlgBuildings()">
              <el-option v-for="c in communities" :key="c.id" :label="c.name" :value="c.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="楼洞" required>
            <el-select v-model="dlg.buildingId" placeholder="请选择" style="width: 100%">
              <el-option v-for="b in dlgBuildings" :key="b.id" :label="b.name" :value="b.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="户号" required>
            <el-input v-model="dlg.roomNo" placeholder="如：101" />
          </el-form-item>
        </template>
        <el-form-item label="户主">
          <el-input v-model="dlg.owner" placeholder="姓名" />
        </el-form-item>
        <el-form-item label="建筑面积" required>
          <el-input-number v-model="dlg.area" :min="0.01" :precision="2" :controls="false" style="width: 100%" />
          <span class="unit">㎡</span>
        </el-form-item>
        <el-form-item label="期初余额">
          <el-input-number v-model="dlg.openingBalance" :precision="2" :controls="false" style="width: 100%"
            :disabled="editingId && dlg.hasVoucher" />
          <span class="unit">元</span>
        </el-form-item>
        <div v-if="editingId && dlg.hasVoucher" class="hint" style="margin: -10px 0 14px 100px">
          该住户已有账务记录，期初余额不可修改
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="savingDlg" @click="saveDlg">保存</el-button>
      </template>
    </el-dialog>

    <!-- 住户流水抽屉 -->
    <el-drawer v-model="stmtVisible" size="620" :title="stmt ? `${stmt.owner || stmt.roomNo} · ${stmt.building}${stmt.roomNo}` : '住户流水'">
      <template v-if="stmt">
        <div class="stmt-info">
          <div><span class="k">小区</span>{{ stmt.community }}</div>
          <div><span class="k">楼洞 / 户号</span>{{ stmt.building }} {{ stmt.roomNo }}</div>
          <div><span class="k">户主</span>{{ stmt.owner || '—' }}</div>
          <div><span class="k">建筑面积</span>{{ Number(stmt.area).toFixed(2) }} ㎡</div>
          <div><span class="k">期初余额</span>¥ {{ fmt(stmt.openingBalance) }}</div>
          <div><span class="k">当前余额</span><b class="money" style="font-size:18px">¥ {{ fmt(stmt.balance) }}</b></div>
        </div>
        <h4 style="margin: 18px 0 10px">历史收支（余额 = 期初逐笔累计）</h4>
        <el-table :data="stmt.lines" size="small" border max-height="480">
          <el-table-column prop="date" label="日期" width="100" />
          <el-table-column label="类型" width="100">
            <template #default="{ row }">
              <el-tag :type="lineTag(row.type)" size="small">{{ lineLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="summary" label="摘要" min-width="150" show-overflow-tooltip />
          <el-table-column label="金额" width="110" align="right">
            <template #default="{ row }">
              <span :style="{ color: lineColor(row.type) }">
                {{ lineSign(row.type) }}{{ fmt(row.amount) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="余额" width="120" align="right">
            <template #default="{ row }">{{ fmt(row.balance) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="80" align="center">
            <template #default="{ row }">
              <el-tag v-if="row.status === 'voided'" type="info" size="small">已作废</el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="!stmt.lines?.length" class="hint" style="margin-top: 10px">暂无收支记录</div>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'
import { nav } from '../store'

const communities = ref([])
const list = ref([])
const keyword = ref('')
const filterCommunity = ref(null)

const dlgVisible = ref(false)
const editingId = ref(0)
const dlgBuildings = ref([])
const dlg = ref({ communityId: null, buildingId: null, roomNo: '', owner: '', area: null, openingBalance: 0, hasVoucher: false })
const stmtVisible = ref(false)
const stmt = ref(null)

function fmt(n) {
  return Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const LINE_LABEL = { income: '缴纳', interest_alloc_child: '利息分配', allocate: '维修分摊', refund: '返还/退返' }
const lineLabel = (t) => LINE_LABEL[t] || t
const linePlus = (t) => t === 'income' || t === 'interest_alloc_child'
const lineTag = (t) => (linePlus(t) ? 'danger' : 'warning')
const lineColor = (t) => (linePlus(t) ? '#a32d2d' : '#3b6d11')
const lineSign = (t) => (linePlus(t) ? '+' : '−')

let searchTimer = null
// 搜索输入防抖：停顿 350ms 后才发请求
function onKeywordInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => loadList(), 350)
}

const loadFailed = ref(false)

async function loadList() {
  const params = {}
  if (filterCommunity.value) params.communityId = filterCommunity.value
  if (keyword.value.trim()) params.keyword = keyword.value.trim()
  try {
    list.value = await api.get('/households', { params })
    loadFailed.value = false
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载住户列表失败：' + e.message)
  }
}

function goImport() {
  nav.page = 'basic'
  ElMessage.info('在「基础数据」页可下载模板并批量导入住户')
}

function openCreate() {
  editingId.value = 0
  dlg.value = { communityId: null, buildingId: null, roomNo: '', owner: '', area: null, openingBalance: 0, hasVoucher: false }
  dlgBuildings.value = []
  dlgVisible.value = true
}

async function openEdit(row) {
  editingId.value = row.id
  dlg.value = {
    communityId: row.communityId, buildingId: null, roomNo: row.roomNo,
    owner: row.owner, area: row.area, openingBalance: row.openingBalance,
    hasVoucher: row.hasVoucher > 0,
  }
  dlgVisible.value = true
}

async function loadDlgBuildings() {
  try {
    dlgBuildings.value = dlg.value.communityId
      ? await api.get('/buildings', { params: { communityId: dlg.value.communityId } }) : []
  } catch (e) { ElMessage.error('加载楼洞失败：' + e.message) }
}

const savingDlg = ref(false)

async function saveDlg() {
  const d = dlg.value
  try {
    if (editingId.value) {
      if (!d.area || d.area <= 0) return ElMessage.error('建筑面积必须大于 0')
    } else {
      if (!d.communityId || !d.buildingId) return ElMessage.error('请选择小区和楼洞')
      if (!d.roomNo.trim()) return ElMessage.error('请填写户号')
      if (!d.area || d.area <= 0) return ElMessage.error('建筑面积必须大于 0')
    }
    savingDlg.value = true
    if (editingId.value) {
      await api.put(`/households/${editingId.value}`, {
        owner: d.owner, area: d.area,
        openingBalance: d.hasVoucher ? undefined : d.openingBalance,
      })
      ElMessage.success('住户信息已更新')
    } else {
      await api.post('/households', {
        communityId: d.communityId, buildingId: d.buildingId, roomNo: d.roomNo.trim(),
        owner: d.owner?.trim(), area: d.area, openingBalance: d.openingBalance || 0,
      })
      ElMessage.success(`住户「${d.roomNo.trim()}」新增成功`)
    }
    dlgVisible.value = false
    await loadList()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    savingDlg.value = false
  }
}

async function openStatement(row) {
  try {
    stmt.value = await api.get(`/households/${row.id}/statement`)
    stmtVisible.value = true
  } catch (e) { ElMessage.error('加载收支流水失败：' + e.message) }
}

async function init() {
  loadFailed.value = false
  try {
    communities.value = await api.get('/communities')
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载小区失败：' + e.message)
  }
  await loadList()
}

onMounted(init)
</script>

<style scoped>
.hint { font-size: 12px; color: #6a7280; }
.money { color: #a32d2d; font-weight: 600; }
.unit { margin-left: 8px; font-size: 13px; color: #374151; }
.stmt-info { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 20px; background: #f6f8fb; border-radius: 8px; padding: 14px 18px; font-size: 14px; }
.stmt-info .k { color: #6a7280; margin-right: 10px; }
</style>
