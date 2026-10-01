import { reactive } from 'vue'

// 跨页面导航状态：首页卡片点击 → 日常录入指定页签；菜单高亮同步
export const nav = reactive({
  page: 'dashboard',
  dailyTab: 'income', // 日常录入页默认页签：income | expense | interest
})

export function goDaily(tab) {
  nav.dailyTab = tab
  nav.page = 'daily'
}
