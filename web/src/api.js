import axios from 'axios'
import { auth } from './store'

const api = axios.create({ baseURL: '/api', timeout: 30000, withCredentials: true })

api.interceptors.response.use(
  (res) => res.data,
  (err) => {
    // 会话失效（登录接口自身的 401 除外）→ 回到登录页
    if (err.response?.status === 401 && !err.config?.url?.includes('/login')) {
      auth.user = ''
      auth.checked = true
    }
    const msg = err.response?.data?.error || err.message || '请求失败'
    return Promise.reject(new Error(msg))
  }
)

export default api
