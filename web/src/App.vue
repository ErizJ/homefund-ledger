<template>
  <div v-if="!auth.checked" class="boot">正在加载…</div>
  <LoginPage v-else-if="!auth.user" />
  <el-container v-else style="min-height: 100vh">
    <el-aside :width="collapsed ? '64px' : '220px'" class="sidebar">
      <div class="logo-row">
        <div v-if="!collapsed" class="logo">🏠 住房维修基金<br />记账系统</div>
        <el-icon class="fold-btn" @click="collapsed = !collapsed">
          <Expand v-if="collapsed" />
          <Fold v-else />
        </el-icon>
      </div>
      <el-menu :default-active="activeKey" :collapse="collapsed" :collapse-transition="false"
        background-color="#1f2937" text-color="#cbd5e1" active-text-color="#ffffff" @select="onSelect">
        <el-menu-item index="home">
          <el-icon><HomeFilled /></el-icon><template #title>首页</template>
        </el-menu-item>
        <el-sub-menu index="m-process">
          <template #title><el-icon><EditPen /></el-icon><span>账务处理</span></template>
          <el-menu-item index="voucherEntry">凭证录入</el-menu-item>
          <el-menu-item index="voucherList">凭证查询</el-menu-item>
          <el-menu-item index="bank">银行对账</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="m-end">
          <template #title><el-icon><Calendar /></el-icon><span>期末业务</span></template>
          <el-menu-item index="periodEnd">结转与月结汇总</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="m-books">
          <template #title><el-icon><TrendCharts /></el-icon><span>账簿查询</span></template>
          <el-menu-item index="gl-generalLedger">总账</el-menu-item>
          <el-menu-item index="gl-entries">明细账</el-menu-item>
          <el-menu-item index="gl-balances">科目余额表</el-menu-item>
          <el-menu-item index="gl-statements">财务报表</el-menu-item>
          <el-menu-item index="gl-reconcile">试算与对账</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="m-biz">
          <template #title><el-icon><Files /></el-icon><span>业务台账</span></template>
          <el-menu-item index="daily">日常录入</el-menu-item>
          <el-menu-item index="households">住户管理</el-menu-item>
          <el-menu-item index="ledger">四级账簿 / 对账单</el-menu-item>
          <el-menu-item index="vouchers">业务凭证</el-menu-item>
          <el-menu-item index="dashboard">收支汇总报表</el-menu-item>
          <el-menu-item index="basic">基础数据导入</el-menu-item>
        </el-sub-menu>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="topbar">
        <span class="tb-title">住宅专项维修资金代管记账</span>
        <span class="tb-org">单位：<b>{{ auth.org || '演示代管单位' }}</b></span>
        <span class="tb-user">当前用户：<b>{{ auth.displayName || auth.user }}</b>（{{ auth.user }}）</span>
        <el-button size="small" link class="tb-logout" @click="logout">退出登录</el-button>
      </el-header>
      <el-main>
        <Dashboard v-if="page === 'dashboard'" />
        <VoucherEntry v-else-if="page === 'voucherEntry'" />
        <VoucherList v-else-if="page === 'voucherList'" />
        <PeriodEnd v-else-if="page === 'periodEnd'" />
        <GLBooks v-else-if="page === 'glbooks'" />
        <DailyEntry v-else-if="page === 'daily'" />
        <Households v-else-if="page === 'households'" />
        <Vouchers v-else-if="page === 'vouchers'" />
        <Ledger v-else-if="page === 'ledger'" />
        <BasicData v-else-if="page === 'basic'" />
        <Bank v-else-if="page === 'bank'" />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import Dashboard from './pages/Dashboard.vue'
import VoucherEntry from './pages/VoucherEntry.vue'
import VoucherList from './pages/VoucherList.vue'
import PeriodEnd from './pages/PeriodEnd.vue'
import GLBooks from './pages/GLBooks.vue'
import DailyEntry from './pages/DailyEntry.vue'
import Households from './pages/Households.vue'
import Vouchers from './pages/Vouchers.vue'
import Ledger from './pages/Ledger.vue'
import BasicData from './pages/BasicData.vue'
import Bank from './pages/Bank.vue'
import LoginPage from './pages/LoginPage.vue'
import { HomeFilled, EditPen, Calendar, TrendCharts, Files, Expand, Fold } from '@element-plus/icons-vue'
import api from './api'
import { nav, auth, setAuth } from './store'

const collapsed = ref(false)

const page = ref(nav.page)
// 首页卡片等组件通过 store 触发跳转（如 goDaily）
watch(() => nav.page, (p) => { page.value = p })

// 菜单高亮：dashboard→首页；glbooks→对应账簿子项
const activeKey = computed(() => {
  if (page.value === 'dashboard') return 'home'
  if (page.value === 'glbooks') return 'gl-' + nav.glTab
  return page.value
})
// 菜单 index → 页面
function onSelect(k) {
  if (k === 'home') {
    page.value = 'dashboard'
  } else if (k.startsWith('gl-')) {
    nav.glTab = k.slice(3)
    page.value = 'glbooks'
  } else {
    page.value = k
  }
  nav.page = page.value
}

async function logout() {
  try {
    await api.post('/logout')
  } catch { /* 会话可能已失效，忽略 */ }
  auth.user = ''
}

onMounted(async () => {
  try {
    const s = await api.get('/session')
    setAuth(s)
  } catch { /* 未登录 */ }
  auth.checked = true
})
</script>

<style>
body { margin: 0; font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f5f6f8; }
.boot { min-height: 100vh; display: flex; align-items: center; justify-content: center; color: #9ca3af; }
.sidebar { background: linear-gradient(180deg, #1f2937 0%, #111827 100%); display: flex; flex-direction: column; }
.logo-row { display: flex; align-items: center; justify-content: space-between; padding: 16px 14px 12px; border-bottom: 1px solid rgba(255, 255, 255, .08); }
.sidebar .logo { color: #fff; font-size: 15px; font-weight: 700; line-height: 1.45; }
.fold-btn { color: #94a3b8; font-size: 18px; cursor: pointer; transition: color .15s; }
.fold-btn:hover { color: #fff; }
.sidebar .el-menu { border-right: none; flex: 1; padding: 8px 6px; }
.sidebar .el-menu-item, .sidebar .el-sub-menu__title { font-size: 14px; height: 42px; line-height: 42px; border-radius: 8px; margin-bottom: 2px; }
.sidebar .el-menu-item:hover, .sidebar .el-sub-menu__title:hover { background: rgba(255, 255, 255, .08) !important; color: #fff !important; }
.sidebar .el-menu-item.is-active { background: linear-gradient(90deg, #2563eb, #1d4ed8) !important; color: #fff !important; font-weight: 600; }
.sidebar .el-sub-menu .el-menu-item { font-size: 13.5px; padding-left: 44px !important; }
.sidebar .el-menu-item [class^="el-icon"], .sidebar .el-sub-menu__title [class^="el-icon"] { font-size: 16px; margin-right: 8px; }
.sidebar .el-menu--collapse .el-menu-item [class^="el-icon"] { margin-right: 0; }
.topbar {
  background: #fff;
  border-bottom: 1px solid #e2e5ea;
  display: flex;
  align-items: center;
  gap: 14px;
  height: 48px;
  padding: 0 20px;
}
.tb-title { font-size: 13px; color: #6a7280; }
.tb-org { margin-left: auto; font-size: 13px; color: #374151; }
.tb-org b { color: #185fa5; }
.tb-user { font-size: 13px; color: #374151; }
.tb-user b { color: #185fa5; }
.tb-logout { font-size: 13px; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.panel { background: #fff; border: 1px solid #e2e5ea; border-radius: 10px; padding: 18px; margin-bottom: 18px; }
.panel h3 { font-size: 15px; margin: 0 0 12px; font-weight: 500; }
</style>
