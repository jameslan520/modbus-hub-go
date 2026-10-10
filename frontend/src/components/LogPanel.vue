<template>
  <div class="log-panel" :class="{panel: !bare}">
    <div v-if="!bare" class="panel-title">
      <span>运行日志</span>
      <div>
        <el-checkbox v-model="autoScroll" size="small">自动滚动</el-checkbox>
        <el-button size="small" @click="emit('clear')">清空</el-button>
      </div>
    </div>
    <div ref="bodyEl" class="log-body mono" :class="{'panel-body': !bare}">
      <div v-for="(l, i) in logs" :key="i" class="log-line" :class="levelClass(l.level)">
        [{{ l.timestamp }}] [{{ l.level }}] {{ l.channelName ? '[' + l.channelName + '] ' : '' }}{{ l.message }}
      </div>
      <el-empty v-if="!logs.length" description="暂无日志" :image-size="60" />
    </div>
  </div>
</template>

<script setup>
import {nextTick, ref, watch} from 'vue'

const props = defineProps({
  logs: {type: Array, default: () => []},
  // bare=true 时由外层容器（标签页）提供边框与标题，避免嵌套两层 panel
  bare: {type: Boolean, default: false}
})
const emit = defineEmits(['clear'])

const bodyEl = ref(null)
const autoScroll = ref(true)

watch(() => props.logs.length, async () => {
  if (!autoScroll.value) return
  await nextTick()
  const el = bodyEl.value
  if (el) el.scrollTop = el.scrollHeight
})

function levelClass(level) {
  switch (level) {
    case 'Error':
      return 'log-line-error'
    case 'Warn':
      return 'log-line-warn'
    default:
      return 'log-line-info'
  }
}
</script>

<style scoped>
.log-panel {
  height: 220px;
  flex-shrink: 0;
  min-height: 0;
}

/* 嵌在标签页里时高度交给外层控制 */
.log-panel:not(.panel) {
  height: 100%;
}

.log-body {
  padding: 6px 10px;
  font-size: 12px;
  line-height: 1.7;
}

.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
