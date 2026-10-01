<template>
  <el-container style="min-height: 100vh">
    <el-aside width="210px" class="sidebar">
      <div class="logo">住房维修基金<br />记账系统</div>
      <el-menu :default-active="activeKey" background-color="#1f2937" text-color="#d1d5db"
        active-text-color="#ffffff" @select="onSelect">
        <el-menu-item index="home">首页</el-menu-item>
        <el-menu-item index="daily">日常录入</el-menu-item>
        <el-menu-item index="households">住户管理</el-menu-item>
        <el-sub-menu index="query">
          <template #title>账务查询</template>
          <el-menu-item index="ledger">四级账簿 / 对账单</el-menu-item>
          <el-menu-item index="vouchers">凭证记录</el-menu-item>
          <el-menu-item index="dashboard">收支汇总报表</el-menu-item>
        </el-sub-menu>
        <el-menu-item index="bank">银行对账</el-menu-item>
        <el-menu-item index="periods">月结锁账</el-menu-item>
        <el-menu-item index="gl">财务账套</el-menu-item>
        <el-menu-item index="basic">基础数据导入</el-menu-item>
      </el-menu>
      <div class="foot">前后端分离版 · SQLite</div>
    </el-aside>
    <el-main>
      <Dashboard v-if="page === 'dashboard'" />
      <DailyEntry v-else-if="page === 'daily'" />
      <Households v-else-if="page === 'households'" />
      <Vouchers v-else-if="page === 'vouchers'" />
      <Ledger v-else-if="page === 'ledger'" />
      <GL v-else-if="page === 'gl'" />
      <BasicData v-else-if="page === 'basic'" />
      <Periods v-else-if="page === 'periods'" />
      <Bank v-else-if="page === 'bank'" />
    </el-main>
  </el-container>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import Dashboard from './pages/Dashboard.vue'
import DailyEntry from './pages/DailyEntry.vue'
import Households from './pages/Households.vue'
import Vouchers from './pages/Vouchers.vue'
import Ledger from './pages/Ledger.vue'
import GL from './pages/GL.vue'
import BasicData from './pages/BasicData.vue'
import Periods from './pages/Periods.vue'
import Bank from './pages/Bank.vue'
import { nav } from './store'

const page = ref(nav.page)
// 首页卡片等组件通过 store 触发跳转（如 goDaily）
watch(() => nav.page, (p) => { page.value = p })

// 菜单高亮：page 为 dashboard 时高亮「首页」
const activeKey = computed(() => (page.value === 'dashboard' ? 'home' : page.value))
// 菜单 index → 页面（index 与页面基本同名）
function onSelect(k) {
  page.value = k === 'home' ? 'dashboard' : k
  nav.page = page.value
}
</script>

<style>
body { margin: 0; font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f5f6f8; }
.sidebar { background: #1f2937; }
.sidebar .logo { color: #fff; font-size: 15px; font-weight: 600; padding: 20px; line-height: 1.5; }
.sidebar .foot { color: #9ca3af; font-size: 12px; padding: 12px 20px; margin-top: 40px; }
.sidebar .el-menu { border-right: none; }
.sidebar .el-menu-item.is-active { background: #185fa5 !important; }
.sidebar .el-menu-item { font-size: 15px; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.panel { background: #fff; border: 1px solid #e2e5ea; border-radius: 10px; padding: 18px; margin-bottom: 18px; }
.panel h3 { font-size: 15px; margin: 0 0 12px; font-weight: 500; }
</style>
