<template>
  <div>
    <h2 style="margin: 0 0 16px">凭证录入</h2>

    <div class="panel">
      <!-- 凭证头：凭证字 / 日期 / 附单据 -->
      <div class="head">
        <span class="word">凭证字：记</span>
        <el-date-picker v-model="date" type="date" value-format="YYYY-MM-DD" style="width: 150px" :clearable="false" />
        <span class="appendix">附单据 <el-input-number v-model="appendix" :min="0" :max="99" size="small" :controls="false" style="width: 56px" /> 张</span>
        <span v-if="lastNo" class="hint">上一张：{{ lastNo }}</span>
      </div>

      <table class="v-table">
        <thead>
          <tr>
            <th style="width: 44px">序号</th>
            <th style="width: 30%">摘要（自填）</th>
            <th style="width: 26%">会计科目</th>
            <th style="width: 15%">借方金额</th>
            <th style="width: 15%">贷方金额</th>
            <th style="width: 12%">项目（小区）</th>
            <th style="width: 40px"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="i">
            <td class="c">{{ i + 1 }}</td>
            <td><el-input v-model="row.summary" placeholder="填写本行业务说明" size="default" /></td>
            <td>
              <el-select v-model="row.subjectCode" filterable placeholder="选择科目" style="width: 100%">
                <el-option v-for="s in subjects" :key="s.code"
                  :label="(s.parent ? '　　' : '') + s.code + ' ' + s.name" :value="s.code" />
              </el-select>
            </td>
            <td class="amt">
              <el-input-number v-model="row.debit" :min="0" :precision="2" :controls="false" style="width: 100%" placeholder="0.00"
                @focus="clearOpposite(row, 'credit')" />
            </td>
            <td class="amt">
              <el-input-number v-model="row.credit" :min="0" :precision="2" :controls="false" style="width: 100%" placeholder="0.00"
                @focus="clearOpposite(row, 'debit')" />
            </td>
            <td>
              <el-select v-model="row.projectId" clearable placeholder="无" style="width: 100%">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </td>
            <td class="c">
              <el-button link type="danger" size="small" :disabled="rows.length <= 2" @click="rows.splice(i, 1)">删</el-button>
            </td>
          </tr>
        </tbody>
        <tfoot>
          <tr class="total">
            <td colspan="3">合　计</td>
            <td class="amt num">{{ fmt(totalDebit) }}</td>
            <td class="amt num">{{ fmt(totalCredit) }}</td>
            <td colspan="2" class="c">
              <el-tag v-if="validRows.length" size="small" :type="balanced ? 'success' : 'danger'">{{ balanced ? '借贷平衡' : '借贷不平' }}</el-tag>
              <el-tag v-else size="small" type="info">待填写</el-tag>
            </td>
          </tr>
        </tfoot>
      </table>

      <div class="ops">
        <el-button @click="addRow">+ 添加一行</el-button>
        <span class="hint">每行只填借方或贷方一边金额；至少一借一贷两行</span>
        <el-button type="primary" size="large" style="margin-left: auto" :disabled="!balanced" @click="save">保存并过账</el-button>
      </div>
    </div>

    <div class="hint" style="margin-top: 12px">
      保存后凭证立即计入总账、明细账、科目余额表；收入/支出类科目在「期末业务」月结时并入净资产。
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import { printVoucher } from '../print'

const date = ref(new Date().toISOString().slice(0, 10))
const appendix = ref(0)
const lastNo = ref('')
const subjects = ref([])
const communities = ref([])
// 初始即给一借一贷两行，保证表格永远不空（接口失败也不影响填写）
const rows = ref([blankRow('debit'), blankRow('credit')])

const fmt = (n) => Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })

function blankRow(direction) {
  return { summary: '', subjectCode: '', projectId: null, debit: null, credit: null, direction }
}
function addRow() { rows.value.push(blankRow(null)) }

// 借/贷互斥：填一边时清空另一边
function clearOpposite(row, opp) {
  if (Number(row[opp] || 0) > 0) row[opp] = null
}

const validRows = computed(() =>
  rows.value.filter((r) => r.subjectCode && (Number(r.debit) > 0 || Number(r.credit) > 0)))
const totalDebit = computed(() => validRows.value.reduce((s, r) => s + Number(r.debit || 0), 0))
const totalCredit = computed(() => validRows.value.reduce((s, r) => s + Number(r.credit || 0), 0))
const balanced = computed(() =>
  validRows.value.length >= 2 &&
  validRows.value.every((r) => !(Number(r.debit) > 0 && Number(r.credit) > 0)) &&
  Math.abs(totalDebit.value - totalCredit.value) < 0.005)

async function save() {
  try {
    const res = await api.post('/gl/manual-voucher', {
      date: date.value,
      appendix: appendix.value || 0,
      entries: validRows.value.map((r) => ({
        summary: r.summary,
        subjectCode: r.subjectCode,
        projectId: r.projectId || 0,
        direction: Number(r.debit) > 0 ? 'debit' : 'credit',
        amount: Number(r.debit) > 0 ? Number(r.debit) : Number(r.credit),
      })),
    })
    lastNo.value = res.no
    ElMessage.success(`记账凭证 ${res.no} 已保存并过账`)
    // 打印记帐凭证（经典版式，含每行摘要）
    const subMap = {}
    for (const s of subjects.value) subMap[s.code] = s
    const parentName = (code) => {
      const s = subMap[code]
      if (!s || !s.parent) return s ? s.name : code
      const p = subMap[s.parent]
      return p ? p.name : s.name
    }
    try {
      await ElMessageBox.confirm(`凭证 ${res.no} 已过账。是否打印记帐凭证？`, '保存成功',
        { confirmButtonText: '打印', cancelButtonText: '不打印', type: 'success' })
      printVoucher({
        no: res.no,
        date: date.value,
        summary: validRows.value[0].summary || '',
        appendix: appendix.value || 0,
        entries: validRows.value.map((r) => {
          const s = subMap[r.subjectCode] || {}
          return {
            summary: r.summary,
            subject: parentName(r.subjectCode),
            sub: (s.parent ? s.name : '') + (r.projectId ? '（' + (communities.value.find((c) => c.id === r.projectId) || {}).name + '）' : ''),
            direction: Number(r.debit) > 0 ? 'debit' : 'credit',
            amount: Number(r.debit) > 0 ? Number(r.debit) : Number(r.credit),
          }
        }),
      })
    } catch { /* 不打印 */ }
    reset()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

function reset() {
  appendix.value = 0
  rows.value = [blankRow('debit'), blankRow('credit')]
}

onMounted(async () => {
  try {
    subjects.value = (await api.get('/gl/subjects')).filter((s) => s.enabled)
    communities.value = await api.get('/communities')
  } catch (e) {
    ElMessage.error('科目/小区基础数据加载失败：' + e.message)
  }
  // 无论基础数据是否加载成功，都保证表格处于可填写状态
  if (!rows.value.length) reset()
})
</script>

<style scoped>
.head { display: flex; gap: 18px; align-items: center; margin-bottom: 12px; }
.word { font-weight: 600; }
.appendix { display: inline-flex; align-items: center; gap: 6px; }
.v-table { width: 100%; border-collapse: collapse; }
.v-table th, .v-table td { border: 1px solid #dcdfe6; padding: 7px 8px; font-size: 13px; }
.v-table th { background: #f5f7fa; font-weight: 600; text-align: center; }
.v-table :deep(.el-input__wrapper) { box-shadow: none; }
.v-table :deep(.el-select__wrapper) { box-shadow: none; }
td.amt { padding: 4px 6px; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.c { text-align: center; }
tfoot .total td { background: #fafcff; font-weight: 600; }
.ops { display: flex; gap: 14px; align-items: center; margin-top: 12px; }
.hint { color: #909399; font-size: 12px; }
</style>
