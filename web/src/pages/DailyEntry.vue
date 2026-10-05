<template>
  <div>
    <h2 style="margin: 0 0 16px">日常录入</h2>
    <ReloadBanner :failed="loadFailed" @retry="init" />

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
              <el-button type="primary" size="large" style="min-width: 140px" :loading="saving" @click="saveIncome">保 存</el-button>
              <el-button size="large" @click="resetForm(inc)">清 空</el-button>
            </el-form-item>
          </el-form>
        </div>

        <!-- 批量导入交存流水 -->
        <div class="entry-card" style="margin-top: 16px">
          <div class="entry-title">批量导入交存流水（银行代收文件）</div>
          <div style="display: flex; gap: 10px; margin-bottom: 10px">
            <el-button @click="downloadIncomeTemplate">下载导入模板(.xlsx)</el-button>
            <el-button type="primary" @click="incBatchFileRef.click()">选择 Excel/CSV 交存流水</el-button>
            <input ref="incBatchFileRef" type="file" accept=".xlsx,.xls,.csv" style="display: none"
              @change="onIncomeBatchFile" />
          </div>
          <div class="hint">
            列头：日期、小区、楼洞、户号、金额、摘要（选填）。按名称匹配小区/楼洞/户号，逐笔生成收款凭证与财务凭证；
            同日同户同金额同摘要自动判重跳过，不存在的户会跳过并在导入结果中列出（导入已结转月份的，需重新执行结转）。
          </div>
          <el-table v-if="batchRows.length" :data="batchRows" size="small" border max-height="260" style="margin-top: 12px">
            <el-table-column prop="date" label="日期" width="110" />
            <el-table-column prop="community" label="小区" min-width="120" />
            <el-table-column prop="building" label="楼洞" width="90" />
            <el-table-column prop="room" label="户号" width="80" />
            <el-table-column label="金额" width="110" align="right">
              <template #default="{ row }">{{ fmt(row.amount) }}</template>
            </el-table-column>
            <el-table-column prop="summary" label="摘要" min-width="140" show-overflow-tooltip />
          </el-table>
          <div v-if="batchRows.length" style="margin-top: 12px; display: flex; gap: 10px; align-items: center">
            <el-button type="primary" size="large" :loading="saving" @click="submitIncomeBatch">
              导入生成凭证（共 {{ batchRows.length }} 笔，合计 ¥ {{ fmt(batchTotal) }}）
            </el-button>
            <el-button size="large" @click="batchRows = []">清 空</el-button>
          </div>
        </div>
      </el-tab-pane>

      <!-- ==================== 录入维修支出 ==================== -->
      <el-tab-pane label="录入维修支出" name="expense">
        <div class="entry-card">
          <div class="entry-title">新增维修支出</div>
          <el-form :model="exp" label-width="110px" class="entry-form">
            <el-form-item label="小区" required>
              <el-select v-model="exp.communityId" placeholder="请选择小区" style="width: 300px" size="large"
                @change="exp.buildingId = null; exp.householdIds = []; loadExpBuildings()">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="分摊范围" required>
              <el-select v-model="exp.buildingId" placeholder="全体楼洞 / 指定楼洞 / 指定住户" style="width: 300px" size="large"
                @change="onExpScopeChange">
                <el-option label="全体楼洞（全小区分摊）" :value="-1" />
                <el-option label="指定住户（多选，如某单元电梯）" :value="-2" />
                <el-option v-for="b in expBuildings" :key="b.id" :label="b.name" :value="b.id" />
              </el-select>
              <span class="hint" style="margin-left: 10px" v-if="expTargetCount >= 0">
                将分摊至 <b>{{ expTargetCount }}</b> 户
              </span>
            </el-form-item>
            <el-form-item v-if="exp.buildingId === -2" label="分摊住户" required>
              <el-select v-model="exp.householdIds" multiple filterable collapse-tags
                placeholder="勾选参与分摊的住户（可搜索户号/户主）" style="width: 460px" size="large">
                <el-option v-for="h in expSelectedPool" :key="h.id"
                  :label="h.building + ' ' + h.roomNo + (h.owner ? '（' + h.owner + '）' : '')" :value="h.id" />
              </el-select>
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
                <el-radio-button value="备用金">备用金</el-radio-button>
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
              <el-button type="primary" size="large" style="min-width: 200px" :loading="saving" @click="saveExpense">保存并生成凭证</el-button>
              <el-button size="large" @click="resetForm(exp)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">
            保存后系统按各户建筑面积占比自动分摊到户（尾差归最后一户），自动生成凭证与分摊明细，无需手工计算。
            分摊前会校验各户余额，涉及余额不足的户将弹出提示，确认后仍可继续。
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
              <el-button type="primary" size="large" style="min-width: 140px" :loading="saving" @click="saveInterest">保 存</el-button>
              <el-button size="large" @click="resetForm(int)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">利息收入计入小区公共账，可在「利息分配」页签按建筑面积分配到各户。</div>
        </div>
      </el-tab-pane>

      <!-- ==================== 利息分配 ==================== -->
      <el-tab-pane label="利息分配" name="interestAlloc">
        <div class="entry-card">
          <div class="entry-title">利息收益分配到户（按建筑面积）</div>
          <el-form :model="ial" label-width="110px" class="entry-form">
            <el-form-item label="小区" required>
              <el-select v-model="ial.communityId" placeholder="请选择小区" style="width: 300px" size="large"
                @change="onAllocCommunity">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="可分配余额">
              <div class="hh-info" style="max-width: 420px">
                <span>公共账余额：<b class="money">¥ {{ fmt(ial.available) }}</b></span>
                <span>其中期初：¥ {{ fmt(ial.publicOpening) }}</span>
              </div>
            </el-form-item>
            <el-form-item label="分配日期" required>
              <el-date-picker v-model="ial.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="分配金额" required>
              <el-input-number v-model="ial.amount" :min="0.01" :precision="2" :controls="false"
                :max="ial.available || undefined" style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元（不超过可分配余额）</span>
            </el-form-item>
            <el-form-item label="分配说明">
              <el-input v-model="ial.remark" style="width: 400px" size="large"
                placeholder="如：2025 年度利息按建筑面积分配" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" style="min-width: 200px" :loading="saving" @click="saveInterestAlloc">保存并分配到户</el-button>
              <el-button size="large" @click="resetForm(ial)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">
            分配后利息从小区公共账转入各户分户账（财务上：借 待分配累计收益 / 贷 维修资金），各户按建筑面积占比入账，尾差归最后一户。
          </div>
        </div>
      </el-tab-pane>

      <!-- ==================== 其他业务（其他收入 / 备用金 / 国债） ==================== -->
      <el-tab-pane label="其他业务" name="others">
        <!-- 其他收入 -->
        <div class="entry-card" style="margin-bottom: 16px">
          <div class="entry-title">其他收入（经营 / 共用设施处置 / 其他）</div>
          <el-form :model="oi" label-width="110px" class="entry-form">
            <el-form-item label="收入类别" required>
              <el-radio-group v-model="oi.incomeKind" size="large">
                <el-radio-button value="business">经营收入</el-radio-button>
                <el-radio-button value="disposal">共用设施处置收入</el-radio-button>
                <el-radio-button value="other">其他收入</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="小区" required>
              <el-select v-model="oi.communityId" placeholder="请选择小区" style="width: 300px" size="large">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="业务日期" required>
              <el-date-picker v-model="oi.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="金额" required>
              <el-input-number v-model="oi.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="oi.remark" style="width: 400px" size="large" placeholder="如：场地租赁、残值回收" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" :loading="saving" @click="saveOtherIncome">保 存</el-button>
              <el-button size="large" @click="resetOtherIncome">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">计入小区公共账可分配收益（财务：借 银行存款 / 贷 对应收入科目），可在「利息分配」页签随收益一并分配到户。</div>
        </div>

        <!-- 备用金 -->
        <div class="entry-card" style="margin-bottom: 16px">
          <div class="entry-title">备用金（提取 / 退回）</div>
          <el-form :model="cs" label-width="110px" class="entry-form">
            <el-form-item label="业务类型" required>
              <el-radio-group v-model="cs.cashKind" size="large">
                <el-radio-button value="withdraw">提取备用金（银行 → 备用金）</el-radio-button>
                <el-radio-button value="return">备用金退回（备用金 → 银行）</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="小区" required>
              <el-select v-model="cs.communityId" placeholder="请选择小区" style="width: 300px" size="large">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="业务日期" required>
              <el-date-picker v-model="cs.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="金额" required>
              <el-input-number v-model="cs.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="cs.remark" style="width: 400px" size="large" placeholder="选填" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" :loading="saving" @click="saveCash">保 存</el-button>
              <el-button size="large" @click="resetCash">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">备用金为资金形态转换，不影响户账与公共账；维修支出选择"备用金"支付时（财务：贷 备用金 1201）。</div>
        </div>

        <!-- 国债 -->
        <div class="entry-card">
          <div class="entry-title">国债投资（购买 / 到期兑付）</div>
          <el-form :model="bd" label-width="110px" class="entry-form">
            <el-form-item label="业务类型" required>
              <el-radio-group v-model="bd.bondKind" size="large">
                <el-radio-button value="buy">购买国债</el-radio-button>
                <el-radio-button value="redeem">到期兑付</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="小区" required>
              <el-select v-model="bd.communityId" placeholder="请选择小区" style="width: 300px" size="large">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="业务日期" required>
              <el-date-picker v-model="bd.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item :label="bd.bondKind === 'redeem' ? '本金' : '购买金额'" required>
              <el-input-number v-model="bd.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item v-if="bd.bondKind === 'redeem'" label="利息收入">
              <el-input-number v-model="bd.interest" :min="0" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元（计入国债利息收入）</span>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="bd.remark" style="width: 400px" size="large" placeholder="如：三年期国债、发行期次" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" :loading="saving" @click="saveBond">保 存</el-button>
              <el-button size="large" @click="resetBond">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">购买：借 国债投资 / 贷 银行存款（国债专户）；到期兑付：借 银行存款（国债专户）/ 贷 国债投资 + 国债利息收入。</div>
        </div>
      </el-tab-pane>

      <!-- ==================== 返还 / 退返 ==================== -->
      <el-tab-pane label="返还/退返" name="refund">
        <div class="entry-card">
          <div class="entry-title">返还 / 退返</div>
          <el-form :model="rf" label-width="110px" class="entry-form">
            <el-form-item label="业务类型" required>
              <el-radio-group v-model="rf.refundKind" size="large">
                <el-radio-button value="destroy">灭失返还（房屋灭失退返维修资金）</el-radio-button>
                <el-radio-button value="return">退返交存（多缴 / 错收退回）</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="小区" required>
              <el-select v-model="rf.communityId" placeholder="请选择小区" style="width: 300px" size="large"
                @change="rf.buildingId = null; rf.householdId = null; loadRfBuildings()">
                <el-option v-for="cm in communities" :key="cm.id" :label="cm.name" :value="cm.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="楼洞" required>
              <el-select v-model="rf.buildingId" placeholder="请选择楼洞" style="width: 300px" size="large"
                @change="rf.householdId = null; loadRfHouseholds()">
                <el-option v-for="b in rfBuildings" :key="b.id" :label="b.name" :value="b.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="户号" required>
              <el-select v-model="rf.householdId" placeholder="请选择住户" style="width: 300px" size="large" filterable>
                <el-option v-for="h in rfHouseholds" :key="h.id"
                  :label="h.roomNo + (h.owner ? '（' + h.owner + '）' : '')" :value="h.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="住户信息">
              <div v-if="rfHouse" class="hh-info">
                <span>户主：<b>{{ rfHouse.owner || '—' }}</b></span>
                <span>建筑面积：<b>{{ rfHouse.area }} ㎡</b></span>
                <span>当前余额：<b class="money">¥ {{ fmt(rfHouse.balance) }}</b></span>
              </div>
              <span v-else class="hint">选择户号后自动显示</span>
            </el-form-item>
            <el-form-item label="业务日期" required>
              <el-date-picker v-model="rf.date" type="date" value-format="YYYY-MM-DD" style="width: 200px" size="large" />
            </el-form-item>
            <el-form-item label="金额" required>
              <el-input-number v-model="rf.amount" :min="0.01" :precision="2" :controls="false"
                style="width: 220px" size="large" placeholder="0.00" />
              <span class="unit">元</span>
            </el-form-item>
            <el-form-item label="备注">
              <el-input v-model="rf.remark" style="width: 400px" size="large" placeholder="如：房屋灭失返还、批文号等" />
            </el-form-item>
            <el-form-item label="附件">
              <el-button size="large" @click="rfFileRef.click()">上传批文 / 退款回单</el-button>
              <input ref="rfFileRef" type="file" style="display: none" @change="(e) => pickFile(e, rf)" />
              <span v-if="rf.file" class="file-ok">已选择：{{ rf.file.name }}</span>
              <span v-else class="hint">选填，保存后自动挂到凭证</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" size="large" style="min-width: 140px" :loading="saving" @click="saveRefund">保 存</el-button>
              <el-button size="large" @click="resetForm(rf)">清 空</el-button>
            </el-form-item>
          </el-form>
          <div class="entry-tip">
            灭失返还记入「返还支出」（借 返还支出 / 贷 银行存款），退返冲减「交存收入」；两者均减少该户分户账余额。
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import * as XLSX from 'xlsx'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
import ReloadBanner from '../components/ReloadBanner.vue'
import { nav } from '../store'

const tab = ref(nav.dailyTab)
watch(() => nav.dailyTab, (t) => { tab.value = t })

// Excel 日期序列号（1900 日期系统）→ YYYY-MM-DD
function excelDate(d) {
  const dt = new Date(Math.round((d - 25569) * 86400 * 1000))
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`
}

function today() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function fmt(n) {
  return Number(n || 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const communities = ref([])
const ledgerCommunities = ref([])
const incBuildings = ref([])
const incHouseholds = ref([])
const expBuildings = ref([])
const expSelectedPool = ref([])
const expTargetCount = ref(-1)
const rfBuildings = ref([])
const rfHouseholds = ref([])
const batchRows = ref([])
const saving = ref(false) // 保存中：按钮 loading + 防重复点击

const incFileRef = ref(null)
const expFileRef = ref(null)
const intFileRef = ref(null)
const rfFileRef = ref(null)
const incBatchFileRef = ref(null)

function blankForm() {
  return { communityId: null, buildingId: null, householdId: null, householdIds: [], date: today(), amount: null,
    method: '银行转账', remark: '', project: '', vendor: '', category: 'engineering', bank: '', file: null }
}
const inc = ref(blankForm())
const exp = ref(blankForm())
const int = ref(blankForm())
const ial = ref({ communityId: null, date: today(), amount: null, remark: '', available: 0, publicOpening: 0 })
const rf = ref({ refundKind: 'destroy', communityId: null, buildingId: null, householdId: null,
  date: today(), amount: null, remark: '', file: null })
const oi = ref({ incomeKind: 'business', communityId: null, date: today(), amount: null, remark: '' })
const cs = ref({ cashKind: 'withdraw', communityId: null, date: today(), amount: null, remark: '' })
const bd = ref({ bondKind: 'buy', communityId: null, date: today(), amount: null, interest: 0, remark: '' })

function resetOtherIncome() { Object.assign(oi.value, { incomeKind: 'business', communityId: null, date: today(), amount: null, remark: '' }) }
function resetCash() { Object.assign(cs.value, { cashKind: 'withdraw', communityId: null, date: today(), amount: null, remark: '' }) }
function resetBond() { Object.assign(bd.value, { bondKind: 'buy', communityId: null, date: today(), amount: null, interest: 0, remark: '' }) }

const incHouse = computed(() => incHouseholds.value.find((h) => h.id === inc.value.householdId) || null)
const rfHouse = computed(() => rfHouseholds.value.find((h) => h.id === rf.value.householdId) || null)
const batchTotal = computed(() => batchRows.value.reduce((s, r) => s + Number(r.amount || 0), 0))

function pickFile(e, form) {
  form.file = e.target.files[0] || null
  e.target.value = ''
}

function resetForm(f) {
  Object.assign(f, blankForm())
  f.file = null
  expTargetCount.value = -1
  exp.value.householdIds = []
  // 利息分配专用字段一并清零（blankForm 未包含，避免残留上一小区的余额）
  if (f === ial.value) Object.assign(f, { available: 0, publicOpening: 0 })
}

async function loadIncBuildings() {
  try {
    incBuildings.value = inc.value.communityId ? await api.get('/buildings', { params: { communityId: inc.value.communityId } }) : []
    incHouseholds.value = []
  } catch (e) { ElMessage.error('加载楼洞失败：' + e.message) }
}
async function loadIncHouseholds() {
  try {
    incHouseholds.value = inc.value.buildingId && inc.value.buildingId > 0
      ? await api.get('/households', { params: { buildingId: inc.value.buildingId } }) : []
  } catch (e) { ElMessage.error('加载住户失败：' + e.message) }
}
function onIncHousehold() { /* 余额在住户信息区展示 */ }

async function loadRfBuildings() {
  try {
    rfBuildings.value = rf.value.communityId ? await api.get('/buildings', { params: { communityId: rf.value.communityId } }) : []
    rfHouseholds.value = []
  } catch (e) { ElMessage.error('加载楼洞失败：' + e.message) }
}
async function loadRfHouseholds() {
  try {
    rfHouseholds.value = rf.value.buildingId && rf.value.buildingId > 0
      ? await api.get('/households', { params: { buildingId: rf.value.buildingId } }) : []
  } catch (e) { ElMessage.error('加载住户失败：' + e.message) }
}

async function loadExpBuildings() {
  try {
    expBuildings.value = exp.value.communityId ? await api.get('/buildings', { params: { communityId: exp.value.communityId } }) : []
    expSelectedPool.value = []
    expTargetCount.value = -1
    if (exp.value.buildingId && exp.value.buildingId !== -2) loadTargetCount()
  } catch (e) { ElMessage.error('加载楼洞失败：' + e.message) }
}
async function onExpScopeChange() {
  exp.value.householdIds = []
  try {
    if (exp.value.buildingId === -2) {
      expSelectedPool.value = exp.value.communityId
        ? await api.get('/households', { params: { communityId: exp.value.communityId } }) : []
      expTargetCount.value = expSelectedPool.value.length
    } else {
      loadTargetCount()
    }
  } catch (e) { ElMessage.error('加载分摊住户失败：' + e.message) }
}
async function loadTargetCount() {
  const bid = exp.value.buildingId
  if (!exp.value.communityId) { expTargetCount.value = -1; return }
  try {
    const rows = bid === -1
      ? await api.get('/households', { params: { communityId: exp.value.communityId } })
      : (bid ? await api.get('/households', { params: { buildingId: bid } }) : [])
    expTargetCount.value = rows.length
  } catch (e) { ElMessage.error('加载分摊户数失败：' + e.message) }
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
  if (saving.value) return
  const f = inc.value
  if (!validate(f, true)) return
  saving.value = true
  const summary = [f.remark?.trim(), '缴款方式：' + f.method].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'income', date: f.date, communityId: f.communityId,
      buildingId: f.buildingId, householdId: f.householdId, amount: f.amount, summary,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
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
  saving.value = false
}

// ==================== 维修支出：分摊预览 + 余额不足确认 ====================

function expScopeInfo() {
  const f = exp.value
  if (f.buildingId === -1) return { scope: 'community', buildingId: 0, text: '全体楼洞（全小区）' }
  if (f.buildingId === -2) return { scope: 'selected', buildingId: 0, text: `指定住户（${f.householdIds.length} 户）` }
  return { scope: 'building', buildingId: f.buildingId,
    text: expBuildings.value.find((b) => b.id === f.buildingId)?.name || '指定楼洞' }
}

async function saveExpense() {
  if (saving.value) return
  const f = exp.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.buildingId) return ElMessage.error('请选择分摊范围（全体楼洞 / 指定楼洞 / 指定住户）')
  if (f.buildingId === -2 && (!f.householdIds || !f.householdIds.length)) return ElMessage.error('请勾选参与分摊的住户')
  if (!f.project?.trim()) return ElMessage.error('请填写维修项目')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  saving.value = true
  const info = expScopeInfo()
  // 预览分摊：金额、户数、各户分摊与余额不足清单
  let pv
  try {
    pv = await api.post('/vouchers/expense-preview', {
      communityId: f.communityId, buildingId: info.buildingId, scope: info.scope,
      householdIds: info.scope === 'selected' ? f.householdIds : [],
      amount: f.amount,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  const insuff = (pv.targets || []).filter((t) => t.deficit > 0)
  let msg = `本次维修支出：¥ ${fmt(f.amount)}\n分摊范围：${info.text}，共 ${pv.targetCount} 户`
  if (insuff.length) {
    const lines = insuff.slice(0, 10).map((t) =>
      `  ${t.roomNo} ${t.owner || ''}：余额 ¥${fmt(t.balance)}，应摊 ¥${fmt(t.share)}，差 ¥${fmt(t.deficit)}`).join('\n')
    msg += `\n\n⚠ 以下 ${insuff.length} 户余额不足：\n${lines}${insuff.length > 10 ? '\n  ……' : ''}\n\n余额不足仍可保存（账面会出现负数），是否继续？`
  } else {
    msg += `\n\n系统将按建筑面积自动分摊至各户，是否确认？`
  }
  try {
    await ElMessageBox.confirm(msg, '确认保存维修支出', {
      confirmButtonText: insuff.length ? '确认继续保存' : '确认保存',
      cancelButtonText: '取消', type: insuff.length ? 'warning' : 'info',
      dangerouslyUseHTMLString: false,
    })
  } catch { saving.value = false; return }
  const summary = [f.project.trim(), f.vendor?.trim() ? '施工单位：' + f.vendor.trim() : '',
    f.remark?.trim(), '付款方式：' + f.method].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'expense', date: f.date, communityId: f.communityId,
      buildingId: info.buildingId, scope: info.scope,
      householdIds: info.scope === 'selected' ? f.householdIds : [],
      amount: f.amount, summary, category: f.category,
      payMethod: f.method === '备用金' ? 'cash' : 'bank',
      confirmInsufficient: insuff.length > 0,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  await attachAfter(res.masterId || res.id, f.file)
  ElMessage.success(`维修支出保存成功！凭证号 ${res.no}，已分摊到 ${res.allocations} 户`, { duration: 5000 })
  Object.assign(f, blankForm())
  f.householdIds = []
  expTargetCount.value = -1
  expSelectedPool.value = []
  saving.value = false
}

async function saveInterest() {
  if (saving.value) return
  const f = int.value
  if (!validate(f, false)) return
  saving.value = true
  const summary = [f.bank?.trim() ? '银行：' + f.bank.trim() : '', f.remark?.trim(), '银行利息'].filter(Boolean).join('；')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'interest', date: f.date, communityId: f.communityId, amount: f.amount, summary,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  await attachAfter(res.id, f.file)
  ElMessage.success(`利息保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
  Object.assign(f, blankForm())
  await loadLedgerCommunities()
  saving.value = false
}

// ==================== 利息分配 ====================

const loadFailed = ref(false)

async function loadLedgerCommunities() {
  try {
    ledgerCommunities.value = await api.get('/ledger/communities')
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载小区公共账失败：' + e.message)
  }
}
async function onAllocCommunity() {
  const row = ledgerCommunities.value.find((c) => c.id === ial.value.communityId)
  ial.value.available = row ? Number(row.publicBalance || 0) : 0
  ial.value.publicOpening = row ? Number(row.publicOpening || 0) : 0
  ial.value.amount = null
}
async function saveInterestAlloc() {
  if (saving.value) return
  const f = ial.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的分配金额')
  if (Number(f.amount) > Number(f.available)) return ElMessage.error(`分配金额超过可分配公共账余额 ¥ ${fmt(f.available)}`)
  saving.value = true
  // 预览户数
  let hhCount = 0
  try {
    const rows = await api.get('/households', { params: { communityId: f.communityId } })
    hhCount = rows.length
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  if (!hhCount) { saving.value = false; return ElMessage.error('该小区没有住户，无法分配') }
  try {
    await ElMessageBox.confirm(
      `将利息 ¥ ${fmt(f.amount)} 按建筑面积分配到「${communities.value.find((c) => c.id === f.communityId)?.name}」的 ${hhCount} 户。\n\n分配后利息从公共账转入各户分户账，是否确认？`,
      '确认利息分配', { confirmButtonText: '确认分配', cancelButtonText: '取消', type: 'info' }
    )
  } catch { saving.value = false; return }
  const summary = ['利息分配', f.remark?.trim()].filter(Boolean).join('：')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'interest_alloc', date: f.date, communityId: f.communityId, amount: f.amount, summary,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  ElMessage.success(`利息分配保存成功！凭证号 ${res.no}，已分配到 ${res.allocations} 户`, { duration: 5000 })
  Object.assign(f, { communityId: null, date: today(), amount: null, remark: '', available: 0, publicOpening: 0 })
  await loadLedgerCommunities()
  saving.value = false
}

// ==================== 返还 / 退返 ====================

async function saveRefund() {
  if (saving.value) return
  const f = rf.value
  if (!validate(f, true)) return
  saving.value = true
  const kindLabel = f.refundKind === 'destroy' ? '灭失返还' : '退返交存'
  try {
    await ElMessageBox.confirm(
      `【${kindLabel}】金额 ¥ ${fmt(f.amount)}\n户室：${rfHouse.value ? rfHouse.value.roomNo + ' ' + (rfHouse.value.owner || '') : ''}\n当前余额：¥ ${fmt(rfHouse.value?.balance || 0)}，扣减后余额：¥ ${fmt(Number(rfHouse.value?.balance || 0) - Number(f.amount))}\n\n是否确认？`,
      '确认保存', { confirmButtonText: '确认保存', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { saving.value = false; return }
  const summary = [kindLabel, f.remark?.trim()].filter(Boolean).join('：')
  let res
  try {
    res = await api.post('/vouchers', {
      type: 'refund', date: f.date, communityId: f.communityId,
      buildingId: f.buildingId, householdId: f.householdId, amount: f.amount, summary,
      refundKind: f.refundKind,
    })
  } catch (e) { saving.value = false; return ElMessage.error(e.message) }
  await attachAfter(res.id, f.file)
  ElMessage.success(`${kindLabel}保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
  Object.assign(f, { refundKind: 'destroy', communityId: null, buildingId: null, householdId: null,
    date: today(), amount: null, remark: '', file: null })
  rfHouseholds.value = []
  saving.value = false
}

// ==================== 其他业务（其他收入 / 备用金 / 国债） ====================

async function saveOtherIncome() {
  if (saving.value) return
  const f = oi.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  saving.value = true
  const kindLabel = { business: '经营收入', disposal: '共用设施处置收入', other: '其他收入' }[f.incomeKind]
  const summary = [kindLabel, f.remark?.trim()].filter(Boolean).join('：')
  try {
    const res = await api.post('/vouchers', {
      type: 'fund_income', date: f.date, communityId: f.communityId,
      amount: f.amount, summary, incomeKind: f.incomeKind,
    })
    resetOtherIncome()
    await loadLedgerCommunities()
    ElMessage.success(`${kindLabel}保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
  } catch (e) { ElMessage.error(e.message) }
  saving.value = false
}

async function saveCash() {
  if (saving.value) return
  const f = cs.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  saving.value = true
  const kindLabel = f.cashKind === 'withdraw' ? '提取备用金' : '备用金退回'
  const summary = [kindLabel, f.remark?.trim()].filter(Boolean).join('：')
  try {
    const res = await api.post('/vouchers', {
      type: 'cash', date: f.date, communityId: f.communityId,
      amount: f.amount, summary, cashKind: f.cashKind,
    })
    ElMessage.success(`${kindLabel}保存成功！凭证号 ${res.no}，¥ ${fmt(f.amount)}`, { duration: 5000 })
    resetCash()
  } catch (e) { ElMessage.error(e.message) }
  saving.value = false
}

async function saveBond() {
  if (saving.value) return
  const f = bd.value
  if (!f.communityId) return ElMessage.error('请选择小区')
  if (!f.amount || f.amount <= 0) return ElMessage.error('请输入正确的金额')
  if (f.bondKind === 'redeem' && Number(f.interest) < 0) return ElMessage.error('利息不能为负')
  saving.value = true
  const kindLabel = f.bondKind === 'buy' ? '购买国债' : '国债到期兑付'
  const summary = [kindLabel, f.remark?.trim()].filter(Boolean).join('：')
  try {
    const res = await api.post('/vouchers', {
      type: 'bond', date: f.date, communityId: f.communityId,
      amount: f.amount, bondInterest: f.bondKind === 'redeem' ? Number(f.interest || 0) : 0,
      summary, bondKind: f.bondKind,
    })
    ElMessage.success(`${kindLabel}保存成功！凭证号 ${res.no}`, { duration: 5000 })
    resetBond()
  } catch (e) { ElMessage.error(e.message) }
  saving.value = false
}

// ==================== 批量导入交存流水 ====================

function downloadIncomeTemplate() {
  const data = [
    { 日期: '2026-09-01', 小区: '阳光花园', 楼洞: '1栋', 户号: '101', 金额: 1200, 摘要: '银行代收' },
    { 日期: '2026-09-01', 小区: '阳光花园', 楼洞: '1栋', 户号: '102', 金额: 1500, 摘要: '银行代收' },
  ]
  const ws = XLSX.utils.json_to_sheet(data)
  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, ws, '交存流水')
  XLSX.writeFile(wb, '交存流水导入模板.xlsx')
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

async function onIncomeBatchFile(e) {
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
      if (text.includes('�')) {
        try { text = new TextDecoder('gbk').decode(buf) } catch { /* utf-8 */ }
      }
      const rows = parseCSV(text)
      if (rows.length < 2) throw new Error('表格没有数据行')
      const header = rows[0].map((h) => h.trim())
      const idx = (names) => header.findIndex((h) => names.some((n) => h.includes(n)))
      const iD = idx(['日期']), iC = idx(['小区']), iB = idx(['楼洞'])
      const iR = idx(['户号', '室号']), iA = idx(['金额']), iS = idx(['摘要', '备注'])
      if (iD < 0 || iC < 0 || iB < 0 || iR < 0 || iA < 0) throw new Error('CSV 缺少必需列头：日期/小区/楼洞/户号/金额')
      json = rows.slice(1).map((r) => ({
        日期: r[iD], 小区: r[iC], 楼洞: r[iB], 户号: r[iR],
        金额: parseFloat(r[iA]) || 0, 摘要: iS >= 0 ? r[iS] : '',
      }))
    }
    if (!json.length) throw new Error('表格没有数据行')
    const rows = json.map((r) => {
      // 日期：Excel 日期单元格是序列号数字，需转换；文本则按原样匹配格式
      let d = r['日期']
      if (typeof d === 'number') d = excelDate(d)
      else d = String(d ?? '').trim()
      const m = d.match(/^(\d{4})[.\-/年](\d{1,2})[.\-/月](\d{1,2})日?$/)
      if (m) d = `${m[1]}-${m[2].padStart(2, '0')}-${m[3].padStart(2, '0')}`
      return {
        date: d, community: String(r['小区'] ?? '').trim(), building: String(r['楼洞'] ?? '').trim(),
        room: String(r['户号'] ?? '').trim(), amount: Number(r['金额'] ?? 0),
        summary: String(r['摘要'] ?? '').trim(),
      }
    })
    batchRows.value = rows
    ElMessage.success(`已读取 ${rows.length} 行，核对后点击「导入生成凭证」`)
  } catch (err) {
    ElMessage.error('读取失败：' + err.message)
  } finally {
    e.target.value = ''
  }
}

async function submitIncomeBatch() {
  if (saving.value) return
  if (!batchRows.value.length) return ElMessage.warning('请先选择交存流水文件')
  saving.value = true
  try {
    await ElMessageBox.confirm(
      `将导入 ${batchRows.value.length} 笔交存流水并逐笔生成收款凭证（合计 ¥ ${fmt(batchTotal.value)}）。\n\n存在问题的行会跳过并在结果中列出。是否继续？`,
      '确认批量导入', { confirmButtonText: '确认导入', cancelButtonText: '取消', type: 'info' }
    )
  } catch { saving.value = false; return }
  try {
    const res = await api.post('/vouchers/import', { rows: batchRows.value })
    let msg = `导入完成：生成凭证 ${res.inserted} 张`
    if (res.firstNo) msg += `（${res.firstNo}${res.lastNo !== res.firstNo ? ' ~ ' + res.lastNo : ''}）`
    if (res.skipped > 0) msg += `，跳过 ${res.skipped} 行：\n` + res.errors.join('\n')
    if (res.skipped > 0) ElMessage({ type: 'warning', message: msg, duration: 10000, showClose: true })
    else ElMessage.success(msg, { duration: 8000 })
    batchRows.value = []
  } catch (e) {
    ElMessage.error('导入失败：' + e.message)
  }
  saving.value = false
}

async function init() {
  loadFailed.value = false
  try {
    communities.value = await api.get('/communities')
  } catch (e) {
    loadFailed.value = true
    ElMessage.error('加载小区失败：' + e.message)
  }
  await loadLedgerCommunities()
}

onMounted(init)
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
