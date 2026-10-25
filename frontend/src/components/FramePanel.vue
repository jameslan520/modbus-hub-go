<template>
  <div class="frame-panel">
    <div class="fp-toolbar">
      <span class="field-label">通道</span>
      <el-select v-model="channelId" size="small" class="sel-ch" @change="load">
        <el-option label="全部通道" value="" />
        <el-option v-for="c in channels" :key="c.id" :label="c.name" :value="c.id" />
      </el-select>

      <el-radio-group v-model="dir" size="small" @change="load">
        <el-radio-button value="">全部</el-radio-button>
        <el-radio-button value="tx">发送</el-radio-button>
        <el-radio-button value="rx">接收</el-radio-button>
      </el-radio-group>

      <el-checkbox v-model="onlyError" size="small" @change="load">只看异常</el-checkbox>

      <el-input
        v-model="keyword"
        size="small"
        class="sel-kw"
        placeholder="按报文或摘要过滤"
        clearable
        @change="load"
        @clear="load"
      />

      <el-checkbox v-model="autoRefresh" size="small">自动刷新</el-checkbox>
      <el-button size="small" @click="load">刷新</el-button>
      <el-button size="small" @click="onExport">导出 CSV</el-button>
      <el-button size="small" type="danger" @click="onClear">清空</el-button>

      <span class="fp-stat">
        共 {{ stats.total }} / {{ stats.capacity }} 条 · 发 {{ stats.tx }} 收 {{ stats.rx }}
        <template v-if="stats.errors"> · <b class="err">异常 {{ stats.errors }}</b></template>
        <template v-if="stats.dropped"> · 已覆盖 {{ stats.dropped }} 条</template>
      </span>
    </div>

    <el-table
      :data="rows"
      size="small"
      border
      stripe
      height="100%"
      class="fp-table"
      row-key="seq"
      :row-class-name="rowClass"
      @row-click="onRowClick"
      @row-dblclick="onRowDblClick"
    >
      <el-table-column prop="time" label="时间" width="96" />
      <el-table-column label="方向" width="58">
        <template #default="{row}">
          <span :class="row.direction === 'tx' ? 'dir-tx' : 'dir-rx'">
            {{ row.direction === 'tx' ? '发送' : '接收' }}
          </span>
        </template>
      </el-table-column>
      <el-table-column v-if="!channelId" prop="channelName" label="通道" width="110" show-overflow-tooltip />
      <el-table-column label="从机" width="56">
        <template #default="{row}">{{ row.slave < 0 ? '--' : row.slave }}</template>
      </el-table-column>
      <el-table-column label="功能码" width="66">
        <template #default="{row}">{{ row.funcCode < 0 ? '--' : funcText(row.funcCode) }}</template>
      </el-table-column>
      <el-table-column prop="hex" label="报文（十六进制）" min-width="240" class-name="mono" show-overflow-tooltip />
      <el-table-column prop="summary" label="摘要" min-width="180" show-overflow-tooltip />
      <el-table-column prop="note" label="说明" min-width="150" show-overflow-tooltip>
        <template #default="{row}">
          <span v-if="row.note" class="note">{{ row.note }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="length" label="字节" width="56" />
    </el-table>

    <div v-if="!rows.length" class="fp-empty">
      {{ hint }}
    </div>

    <!-- 单条报文的完整解析 -->
    <el-dialog v-model="detailVisible" :title="detailTitle" width="620px" append-to-body>
      <div v-if="detail" class="fp-detail">
        <div class="dl-row">
          <label>时间</label><b>{{ detail.time }}</b>
          <label>方向</label><b>{{ detail.direction === 'tx' ? '发送' : '接收' }}</b>
        </div>
        <div class="dl-row">
          <label>通道</label><b>{{ detail.channelName || '--' }}</b>
          <label>字节数</label><b>{{ detail.length }}</b>
        </div>
        <div class="dl-hex">
          <div class="hex-line mono">{{ detail.hex }}</div>
          <div class="ascii-line mono">{{ detail.ascii }}</div>
        </div>
        <div class="dl-summary">{{ detail.summary }}</div>
        <div v-if="detail.note" class="dl-note">{{ detail.note }}</div>
        <el-table :data="detail.fields || []" size="small" border class="dl-table">
          <el-table-column prop="name" label="字段" width="110" />
          <el-table-column prop="hex" label="十六进制" min-width="130" class-name="mono" />
          <el-table-column prop="text" label="含义" min-width="180" />
        </el-table>
      </div>
      <template #footer>
        <el-button size="small" @click="copyHex">复制报文</el-button>
        <el-button size="small" type="primary" @click="detailVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {ElMessage, ElMessageBox} from 'element-plus'
import {
  ClearFrames,
  ExportFrames,
  GetFrameDetail,
  GetFrameStats,
  GetFrames
} from '../../wailsjs/go/main/App'

const props = defineProps({
  channels: {type: Array, default: () => []},
  // 界面上正在看哪个通道，切标签页时用它当默认过滤
  currentId: {type: String, default: ''}
})

const rows = ref([])
const stats = ref({total: 0, dropped: 0, capacity: 0, tx: 0, rx: 0, errors: 0})
const channelId = ref('')
const dir = ref('')
const onlyError = ref(false)
const keyword = ref('')
const autoRefresh = ref(true)
const hint = ref('暂无报文')

const detailVisible = ref(false)
const detail = ref(null)

let timer = 0

const detailTitle = computed(() => {
  if (!detail.value) return '报文详情'
  return `报文详情 · ${detail.value.direction === 'tx' ? '发送' : '接收'} ${detail.value.time}`
})

async function load() {
  const kw = keyword.value.trim()
  const list = (await GetFrames(channelId.value, dir.value, kw)) || []
  // "只看异常"是纯前端过滤，后端仍按关键字返回全量，切换时不用再跑一趟
  rows.value = onlyError.value ? list.filter(r => r.level !== 'ok') : list
  stats.value = await GetFrameStats()
  hint.value = channelId.value || dir.value || kw || onlyError.value
    ? '当前过滤条件下没有报文'
    : '暂无报文。启动通道采集后，这里会实时显示每一帧收发内容。'
}

function funcText(fc) {
  return '0x' + Number(fc).toString(16).toUpperCase().padStart(2, '0')
}

function rowClass({row}) {
  if (row.level === 'error') return 'row-error'
  if (row.level === 'warn') return 'row-warn'
  return ''
}

// 单击复制报文，双击看完整解析：现场最常用的两个动作，别藏进菜单里
async function onRowClick(row) {
  const text = row.hex
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success({message: '已复制报文', duration: 1000})
  } catch (e) {
    ElMessage.warning('复制失败，请手动选中文本复制')
  }
}

async function onRowDblClick(row) {
  try {
    detail.value = await GetFrameDetail(row.seq)
    detailVisible.value = true
  } catch (e) {
    ElMessage.warning(String(e))
  }
}

async function copyHex() {
  if (!detail.value) return
  try {
    await navigator.clipboard.writeText(detail.value.hex)
    ElMessage.success({message: '已复制报文', duration: 1000})
  } catch (e) {
    ElMessage.warning('复制失败，请手动选中文本复制')
  }
}

async function onExport() {
  try {
    const path = await ExportFrames(channelId.value, dir.value, keyword.value.trim())
    if (path) ElMessage.success(`已导出 ${path}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function onClear() {
  const scope = channelId.value ? '该通道' : '全部'
  try {
    await ElMessageBox.confirm(`清空${scope}报文记录？清空后无法恢复。`, '清空报文', {type: 'warning'})
  } catch (e) {
    return
  }
  await ClearFrames(channelId.value)
  rows.value = []
  await load()
}

onMounted(async () => {
  channelId.value = props.currentId
  await load()
  timer = window.setInterval(() => {
    if (autoRefresh.value) load()
  }, 1000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})

// 主通道切换时跟着换过滤条件，省得用户每次手动选
watch(() => props.currentId, v => {
  if (v !== channelId.value && rows.value.length === 0) {
    channelId.value = v
    load()
  }
})
</script>

<style scoped>
.frame-panel {
  position: relative;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.fp-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 6px 8px;
  border-bottom: 1px solid #e2e8f0;
}

.field-label {
  font-size: 12px;
  color: #606266;
}

.sel-ch {
  width: 140px;
}

.sel-kw {
  width: 180px;
}

.fp-stat {
  margin-left: auto;
  font-size: 12px;
  color: #7a869a;
  font-family: Consolas, monospace;
}

.fp-stat .err {
  color: #c45656;
}

.fp-table {
  flex: 1;
  min-height: 0;
}

.fp-table :deep(.mono) {
  font-family: Consolas, monospace;
}

.dir-tx {
  color: #2f6fd0;
}

.dir-rx {
  color: #1f9d55;
}

.note {
  color: #b06f1e;
}

.fp-table :deep(.row-error) {
  background: #fef0f0;
}

.fp-table :deep(.row-warn) {
  background: #fdf6ec;
}

.fp-empty {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  top: 92px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: #9aa5b5;
  pointer-events: none;
}

.fp-detail .dl-row {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding: 2px 0;
}

.fp-detail .dl-row label {
  width: 44px;
  font-size: 12px;
  color: #7a869a;
}

.fp-detail .dl-row b {
  font-family: Consolas, monospace;
  font-size: 13px;
  color: #24303f;
  min-width: 90px;
}

.dl-hex {
  margin: 8px 0;
  padding: 8px 10px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
}

.hex-line {
  font-size: 13px;
  color: #1f6f3f;
  word-break: break-all;
  letter-spacing: 0.5px;
}

.ascii-line {
  margin-top: 4px;
  font-size: 12px;
  color: #9aa5b5;
  word-break: break-all;
}

.dl-summary {
  font-size: 13px;
  color: #24303f;
  margin-bottom: 4px;
}

.dl-note {
  font-size: 12px;
  color: #b06f1e;
  margin-bottom: 8px;
}

.dl-table :deep(.mono) {
  font-family: Consolas, monospace;
}
</style>
