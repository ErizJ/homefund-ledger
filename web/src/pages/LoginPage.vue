<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="logo">住房维修基金<br />记账系统</div>
      <p class="sub">住宅专项维修资金代管记账 · 财会〔2020〕7号</p>
      <el-form @submit.prevent>
        <el-form-item>
          <el-input v-model="username" placeholder="用户名" size="large" :prefix-icon="User" />
        </el-form-item>
        <el-form-item>
          <el-input v-model="password" placeholder="密码" type="password" show-password size="large"
            :prefix-icon="Lock" @keyup.enter="doLogin" />
        </el-form-item>
        <el-button type="primary" size="large" style="width: 100%" :loading="loading" @click="doLogin">
          登 录
        </el-button>
      </el-form>
      <p class="err" v-if="errMsg">{{ errMsg }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { User, Lock } from '@element-plus/icons-vue'
import api from '../api'
import { setAuth } from '../store'

const username = ref('')
const password = ref('')
const loading = ref(false)
const errMsg = ref('')

async function doLogin() {
  if (!username.value.trim() || !password.value) {
    errMsg.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  errMsg.value = ''
  try {
    const res = await api.post('/login', { username: username.value.trim(), password: password.value })
    setAuth(res)
  } catch (e) {
    errMsg.value = e.message
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(160deg, #1f2937 0%, #374151 60%, #4b5563 100%);
}
.login-card {
  width: 380px;
  background: #fff;
  border-radius: 14px;
  padding: 40px 36px 32px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, .35);
}
.logo {
  text-align: center;
  font-size: 22px;
  font-weight: 700;
  color: #1f2937;
  line-height: 1.5;
  margin-bottom: 6px;
}
.sub {
  text-align: center;
  font-size: 12px;
  color: #9ca3af;
  margin: 0 0 26px;
}
.err {
  margin: 14px 0 0;
  text-align: center;
  color: #c0392b;
  font-size: 13px;
}
</style>
