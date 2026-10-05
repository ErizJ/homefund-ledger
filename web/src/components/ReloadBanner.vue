<template>
  <div v-if="failed" class="reload-banner">
    <span>⚠ 部分数据加载失败，页面内容可能不完整。</span>
    <el-button size="small" type="primary" link @click="$emit('retry')">重试加载</el-button>
  </div>
</template>

<script setup>
defineProps({ failed: { type: Boolean, default: false } })
defineEmits(['retry'])
</script>

<style scoped>
.reload-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  background: #fdf6ec;
  border: 1px solid #f5dab1;
  color: #8a6d3b;
  border-radius: 8px;
  padding: 10px 16px;
  margin-bottom: 16px;
  font-size: 13px;
}
</style>
