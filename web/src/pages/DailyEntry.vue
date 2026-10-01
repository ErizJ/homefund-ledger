<template>
  <div>
    <h2 style="margin: 0 0 16px">日常录入</h2>

    <el-tabs v-model="tab">
      <!-- ==================== 录入收款 ==================== -->
      <el-tab-pane label="录入收款" name="income">
        <div class="entry-card">
          <div class="entry-title">新增维修基金收款</div>
          <el-form :model="inc" label-width="110px" label-position="right" class="entry-form">
            <el-form-item label="小区" required>
              <el-select v-model="inc.communityId" placeholder="请选择小区" style="width: 300px" size="large"
                @change="inc.buildingId = null; inc.householdId = null; loadIncBuildings()">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="楼洞" required>
              <el-select v-model="inc.buildingId" placeholder="请选择楼洞" style="width: 300px" size="large"
                @change="inc.householdId = null; loadIncHouseholds()">
                <el-option v-for="b in incBuildings" :key="b.id" :label="b.name" :value="b.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="户号" required>
              <el-select v-model="inc.householdId" placeholder="请选择住户（可输入户号搜索）" style="width: 300px" size="large"
                filterable @change="onIncHousehold">
                <el-option v-for="h in incHouseholds" :key="h.id"
                  :label="h.roomNo + (h.owner ? '（' + h.owner + '）' : '')" :value="h.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="住户信息">
              <div v-if="incHouse" class="hh-info">
                <span>户主：<b>{{ incHouse.owner || '—' }}</b></span>
                <span>建筑面积：<b>{{ incHouse.area }} ㎡</b></span>
                <span>当前余额：<b class="money">¥ {{ fmt(incHouse.balance) }}</b></span>
              </div>
              <span v-else class="hint">选择户号后自动显示</span>
            </el-form-item>
            <el-form-item label="缴款日期" required>
              <el-date-picker v-model="inc.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="缴款金额" required>
              <el-input-number v-model="inc.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="缴款方式">
              <el-radio-group v-model="inc.method" size="large">
                <el-radio-button value="银行转账">银行转账</el-radio-button>
                <el-radio-button value="现金">现金</el-radio-button>
                <el-radio-button value="其他">其他</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="inc.remark" style="width: 400px" placeholder="选填" />
            </el-form-item>
            <el-form-item label="附件">
              <el-button size="large" @click="incFileRef.click()">上传收据 / 银行回单</el-button>
              <input ref="incFileRef" type="file" style="display: none" @change="(e) => pickFile(e, inc)" />
              <span v-if="inc.file" class="file-ok">已选择：{{ inc.file.name }}</span>
              <span v-else class="hint">选填，保存后自动挂到凭证</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" style="min-width: 140px" @click="saveIncome">保 存</el-button>
              <el-button size="large" @click="resetForm(inc)">清 空</el-button>
            </el-form-item>
          </el-form>
        </div>
      </el-tab-pane>

      <!-- ==================== 录入维修支出 ==================== -->
      <el-tab-pane label="录入维修支出" name="expense">
        <div class="entry-card">
          <div class="entry-title">新增维修支出</div>
          <el-form :model="exp" label-width="110px" class="entry-form">
            <el-form-item label="小区" required>
              <el-select v-model="exp.communityId" placeholder="请选择小区" style="width: 300px" size="large"
                @change="exp.buildingId = null; loadExpBuildings()">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="分摊范围" required>
              <el-select v-model="exp.buildingId" placeholder="全体楼洞 / 指定楼洞" style="width: 300px" size="large"
                @change="loadTargetCount">
                <el-option label="全体楼洞（全小区分摊）" :value="-1" />
                <el-option v-for="b in expBuildings" :key="b.id" :label="b.name" :value="b.id" />
              </el-select>
              <span class="hint" style="margin-left: 10px" v-if="expTargetCount >= 0">
                将分摊至 <b>{{ expTargetCount }}</b> 户
              </span>
            </el-form-item>
            <el-form-item label="维修日期" required>
              <el-date-picker v-model="exp.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="维修项目" required>
              <el-input v-model="exp.project" style="width: 400px" size="large" placeholder="如：2栋电梯大修" />
            </el-form-item>
            <el-form-item label="维修单位">
              <el-input v-model="exp.vendor" style="width: 400px" size="large" placeholder="施工单位名称，选填" />
            </el-form-item>
            <el-form-item label="费用类别" required>
              <el-select v-model="exp.category" style="width: 300px" size="large">
                <el-option label="工程维修费" value="engineering" />
                <el-option label="监理费" value="supervision" />
                <el-option label="检测费、勘察设计费" value="survey" />
                <el-option label="其他维修相关费用" value="other" />
              </el-select>
            </el-form-item>
            <el-form-item label="支出金额" required>
              <el-input-number v-model="exp.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="付款方式">
              <el-radio-group v-model="exp.method" size="large">
                <el-radio-button value="银行转账">银行转账</el-radio-button>
                <el-radio-button value="其他">其他</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="维修说明">
              <el-input v-model="exp.remark" type="textarea" :rows="2" style="width: 400px" placeholder="选填" />
            </el-form-item>
            <el-form-item label="附件">
              <el-button size="large" @click="expFileRef.click()">上传发票 / 合同 / 付款凭证</el-button>
              <input ref="expFileRef" type="file" style="display: none" @change="(e) => pickFile(e, exp)" />
              <span v-if="exp.file" class="file-ok">已选择：{{ exp.file.name }}</span>
              <span v-else class="hint">选填，保存后自动挂到凭证</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" style="min-width: 200px" @click="saveExpense">保存并生成凭证</el-button>
              <el-button size="large" @click="resetForm(exp)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">
            保存后系统按各户建筑面积占比自动分摊到户（尾差归最后一户），自动生成凭证与分摊明细，无需手工计算。
          </div>
        </div>
      </el-tab-pane>

      <!-- ==================== 录入利息 ==================== -->
      <el-tab-pane label="录入利息" name="interest">
        <div class="entry-card">
          <div class="entry-title">新增利息收入</div>
          <el-form :model="int" label-width="110px" class="entry-form">
            <el-form-item label="小区" required>
              <el-select v-model="int.communityId" placeholder="请选择小区" style="width: 300px" size="large">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="收入日期" required>
              <el-date-picker v-model="int.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="利息金额" required>
              <el-input-number v-model="int.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="银行">
              <el-input v-model="int.bank" style="width: 400px" size="large" placeholder="如：工商银行 XX 支行" />
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="int.remark" style="width: 400px" placeholder="选填" />
            </el-form-item>
            <el-form-item label="附件">
              <el-button size="large" @click="intFileRef.click()">上传银行回单</el-button>
              <input ref="intFileRef" type="file" style="display: none" @change="(e) => pickFile(e, int)" />
              <span v-if="int.file" class="file-ok">已选择：{{ int.file.name }}</span>
              <span v-else class="hint">选填，保存后自动挂到凭证</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" style="min-width: 140px" @click="saveInterest">保 存</el-button>
              <el-button size="large" @click="resetForm(int)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">利息收入计入小区公共账，不向住户分摊。</div>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { nav } from '../store'

const tab = ref(nav.dailyTab)
watch(() => nav.dailyTab, (t) => { tab.value = t })

function today() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function fmt(n) {
  return Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const communities = ref([])
const incBuildings = ref([])
const incHouseholds = ref([])
const expBuildings = ref([])
const expTargetCount = ref(-1)

const incFileRef = ref(null)
const expFileRef = ref(null)
const intFileRef = ref(null)

function blankForm() {
  return { communityId: null, buildingId: null, householdId: null, date: today(), amount: null,
    method: '银行转账', remark: '', project: '', vendor: '', category: 'engineering', bank: '', file: null }
}
const inc = ref(blankForm())
const exp = ref(blankForm())
const int = ref(blankForm())

const incHouse = computed(() => incHouseholds.value.find((h) => h.id === inc.value.householdId) || null)

function pickFile(e, form) {
  form.file = e.target.files[0] || null
  e.target.value = ''
}

function resetForm(f) {
  const file = f.file
  Object.assign(f, blankForm())
  f.file = null
}

async function loadIncBuildings() {
  incBuildings.value = inc.value.communityId ? await api.get('/buildings', { params: { communityId: inc.value.communityId } }) : []
  incHouseholds.value = []
}
async function loadIncHouseholds() {
  incHouseholds.value = inc.value.buildingId && inc.value.buildingId > 0
    ? await api.get('/households', { params: { buildingId: inc.value.buildingId } }) : []
}
async function loadExpBuildings() {
  expBuildings.value = exp.value.communityId ? await api.get('/buildings', { params: { communityId: exp.value.communityId } }) : []
  expTargetCount.value = -1
  if (exp.value.buildingId) loadTargetCount()
}
async function loadTargetCount() {
  const bid = exp.value.buildingId
  if (!exp.value.communityId) { expTargetCount.value = -1; return }
  const rows = bid === -1
    ? await api.get('/households', { params: { communityId: exp.value.communityId } })
    : (bid ? await api.get('/households', { params: { buildingId: bid } }) : [])
  expTargetCount.value = rows.length
}

// 保存成功后统一：提示 + 传附件
async function attachAfter(voucherId, file) {
  if (!file || !voucherId) return
  const fd = new FormData()
  fd.append('file', file)
  try {
    await api.post(`/vouchers/${voucherId}/attachments`, fd, { headers: { 'Content-Type': 'multipart/form-data' } })
    ElMessage.success('附件已上传')
  } catch (e) {
    ElMessage.warning('凭证已保存，但附件上传失败：' + e.message)
  }
}

function validate(f, needHousehold) {
  if (!f.communityId) { ElMessage.error('请选择小区'); return false }
  if (!f.amount || f.amount <= 0) { ElMessage.error('请输入正确的金额'); return false }
  if (needHousehold && (!f.buildingId || !f.householdId)) { ElMessage.error('请选择楼洞和户号'); return false }
  return true
}

async function saveIncome() {
  const f = inc.value
  if (!validate(f, true)) return
  const summary = [f.remark?.trim(), '缴款方式：' + f.method].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'income', date: f.date, communityId: f.communityId,
      buildingId: f.buildingId, householdId: f.householdId, amount: f.amount, summary,
    })
  } catch (e) { return ElMessage.error(e.message) }
  await attachAfter(res.id, f.file)
  const house = incHouse.value
  ElMessage.success(`收款保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
  const keepCommunity = f.communityId, keepBuilding = f.buildingId
  Object.assign(f, blankForm())
  f.communityId = keepCommunity; f.buildingId = keepBuilding
  await loadIncBuildings(); await loadIncHouseholds()
  if (house) {
    const again = incHouseholds.value.find((h) => h.id === house.id)
    if (again) { f.householdId = again.id; ElMessage.info(`${again.roomNo} 当前余额：¥ ${fmt(again.balance)}`) }
  }
}

async function saveExpense() {
  const f = exp.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.buildingId) return ElMessage.error('请选择分摊范围（全体楼洞或指定楼洞）')
  if (!f.project?.trim()) return ElMessage.error('请填写维修项目')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  await loadTargetCount()
  const scopeText = f.buildingId === -1 ? '全体楼洞' : (expBuildings.value.find((b) => b.id === f.buildingId)?.name || '指定楼洞')
  try {
    await ElMessageBox.confirm(
      `本次维修支出：¥ ${fmt(f.amount)}\n分摊范围：${scopeText}，共 ${expTargetCount.value} 户\n\n系统将按建筑面积自动分摊至各户，是否确认？`,
      '确认保存维修支出', { confirmButtonText: '确认保存', cancelButtonText: '取消', type: 'info' }
    )
  } catch { return }
  const summary = [f.project.trim(), f.vendor?.trim() ? '施工单位：' + f.vendor.trim() : '',
    f.remark?.trim(), '付款方式：' + f.method].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'expense', date: f.date, communityId: f.communityId,
      buildingId: f.buildingId, scope: f.buildingId === -1 ? 'community' : 'building',
      amount: f.amount, summary, category: f.category,
    })
  } catch (e) { return ElMessage.error(e.message) }
  await attachAfter(res.masterId || res.id, f.file)
  ElMessage.success(`维修支出保存成功！凭证号 ${res.no}，已分摊到 ${res.allocations} 户`, { duration: 5000 })
  Object.assign(f, blankForm())
  expTargetCount.value = -1
}

async function saveInterest() {
  const f = int.value
  if (!validate(f, false)) return
  const summary = [f.bank?.trim() ? '银行：' + f.bank.trim() : '', f.remark?.trim(), '银行利息'].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'interest', date: f.date, communityId: f.communityId, amount: f.amount, summary,
    })
  } catch (e) { return ElMessage.error(e.message) }
  await attachAfter(res.id, f.file)
  ElMessage.success(`利息保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
  Object.assign(f, blankForm())
}

onMounted(async () => {
  communities.value = await api.get('/communities')
})
</script>

<style scoped>
.entry-card { background: #fff; border: 1px solid #e2e5ea; border-radius: 10px; padding: 28px 36px; max-width: 760px; }
.entry-title { font-size: 19px; font-weight: 600; margin-bottom: 22px; color: #1f2937; }
.entry-form :deep(.el-form-item__label) { font-size: 15px; }
.entry-form :deep(.el-form-item) { margin-bottom: 22px; }
.hh-info { display: flex; gap: 26px; font-size: 14px; background: #f6f8fb; border-radius: 8px; padding: 10px 16px; }
.hh-info .money { color: #a32d2d; }
.unit { margin-left: 8px; font-size: 15px; color: #374151; }
.hint { font-size: 13px; color: #6a7280; }
.file-ok { margin-left: 10px; font-size: 13px; color: #2e7d32; }
.entry-tip { background: #fdf6ec; border-radius: 8px; padding: 10px 16px; font-size: 13px; color: #8a6d3b; margin-top: 4px; }
</style>
