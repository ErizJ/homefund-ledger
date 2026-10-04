import { reactive } from 'vue'

// 跨页面导航状态：首页卡片点击 → 日常录入指定页签；菜单高亮同步
export const nav = reactive({
  page: 'dashboard',
  dailyTab: 'income', // 日常录入页默认页签：income | expense | interest | interestAlloc | refund
  glTab: 'generalLedger', // 账簿查询页默认页签：generalLedger | entries | balances | statements | reconcile
})

// 登录状态：checked=是否已向服务端确认会话；user=当前用户名（空=未登录）
export const auth = reactive({
  checked: false,
  user: '',
  displayName: '',
  org: '',
})

export function setAuth(session) {
  auth.user = session?.user || ''
  auth.displayName = session?.displayName || auth.user
  auth.org = session?.org || ''
}

export function goDaily(tab) {
  nav.dailyTab = tab
  nav.page = 'daily'
}
