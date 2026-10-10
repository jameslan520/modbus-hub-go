<template>
  <div class="app-shell">
    <div class="app-header">
      <span class="app-title">Modbus RTU 多通道采集系统</span>
      <div class="app-actions">
        <el-button type="primary" size="small" @click="onStartAll">全部启动</el-button>
        <el-button size="small" @click="onStopAll">全部停止</el-button>
        <el-button size="small" @click="onSave">保存配置</el-button>
        <el-button size="small" :disabled="!currentChannel" @click="onCalibration">传感器标定</el-button>
        <el-button size="small" :disabled="!currentChannel" @click="onHistory">历史回放</el-button>
        <el-dropdown size="small" trigger="click" @command="onExportCommand">
          <el-button size="small">导出<el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="tags" :disabled="!currentChannel">当前通道点表</el-dropdown-item>
              <el-dropdown-item command="tagdef" :disabled="!currentChannel">点位定义（可再导入）</el-dropdown-item>
              <el-dropdown-item command="alarms">报警记录</el-dropdown-item>
              <el-dropdown-item command="logs">运行日志</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-badge :value="activeAlarms.length" :hidden="!activeAlarms.length" type="danger">
          <el-button size="small" @click="bottomTab = 'alarm'">报警</el-button>
        </el-badge>
        <span class="app-clock">{{ clock }}</span>
      </div>
    </div>

    <div class="app-body">
      <ChannelList
        :channels="channels"
        :runtime="runtimeMap"
        :current-id="currentId"
        @select="onSelect"
        @add="onAddChannel"
        @edit="onEditChannel"
        @remove="onRemoveChannel"
        @start="onStart"
        @stop="onStop"
      />

      <div class="app-main">
        <TagTable
          :channel="currentChannel"
          :values="currentValues"
          @add="onAddTag"
          @edit="onEditTag"
          @remove="onRemoveTag"
          @write="onWriteTag"
          @batch-write="onBatchWrite"
          @trend="onTrend"
          @history="onHistory"
          @compare="onCompare"
          @import="onImportTags"
          @export-def="onExportDef"
        />

        <div class="panel bottom-panel">
          <el-tabs v-model="bottomTab" class="bottom-tabs">
            <el-tab-pane label="运行日志" name="log">
              <LogPanel bare :logs="logs" @clear="logs = []" />
            </el-tab-pane>
            <el-tab-pane name="alarm">
              <template #label>
                报警
                <el-tag v-if="activeAlarms.length" size="small" type="danger" effect="dark" round>
                  {{ activeAlarms.length }}
                </el-tag>
              </template>
              <AlarmPanel
                :active="activeAlarms"
                :history="alarmHistory"
                @ack="onAck"
                @ack-all="onAckAll"
                @clear="onClearAlarmHistory"
                @export="onExportCommand('alarms')"
                @refresh="refreshAlarms"
              />
            </el-tab-pane>
          </el-tabs>
        </div>
      </div>
    </div>

    <ChannelDialog
      v-model:visible="channelDialog.visible"
      :channel="channelDialog.data"
      :is-new="channelDialog.isNew"
      @submit="onChannelSubmit"
    />
    <TagDialog
      v-model:visible="tagDialog.visible"
      :tag="tagDialog.data"
      :is-new="tagDialog.isNew"
      :channel="currentChannel"
      @submit="onTagSubmit"
    />
    <TagImportDialog
      v-model:visible="importDialog.visible"
      :channel="currentChannel"
      @imported="onImported"
    />
    <HistoryDialog
      v-model:visible="historyDialog.visible"
      :tag="historyDialog.tag"
      :channel="currentChannel"
    />
    <BatchWriteDialog
      v-model:visible="batchDialog.visible"
      :tags="batchDialog.tags"
      @submit="onBatchSubmit"
    />
    <TrendDialog
      v-model:visible="trendDialog.visible"
      :tag="trendDialog.tag"
      :channel="currentChannel"
      :compare-ids="compareIds"
      :values="currentValues"
    />
    <CalibrationDialog
      v-model:visible="calibDialog.visible"
      :channel="currentChannel"
      :values="currentValues"
      :running="currentRunning"
      @update="onCalibUpdate"
    />
  </div>
</template>

<script setup>
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import {ElMessage} from 'element-plus'
import {ArrowDown} from '@element-plus/icons-vue'
import ChannelList from './components/ChannelList.vue'
import ChannelDialog from './components/ChannelDialog.vue'
import TagTable from './components/TagTable.vue'
import TagDialog from './components/TagDialog.vue'
import TagImportDialog from './components/TagImportDialog.vue'
import BatchWriteDialog from './components/BatchWriteDialog.vue'
import TrendDialog from './components/TrendDialog.vue'
import HistoryDialog from './components/HistoryDialog.vue'
import CalibrationDialog from './components/CalibrationDialog.vue'
import LogPanel from './components/LogPanel.vue'
import AlarmPanel from './components/AlarmPanel.vue'
import {
  AckAlarm,
  AckAllAlarms,
  ClearAlarmHistory,
  ClearHistory,
  ExportAlarms,
  ExportLogs,
  ExportTagDefinitions,
  ExportTags,
  GetActiveAlarms,
  GetAlarmHistory,
  GetChannels,
  GetLogs,
  GetRuntime,
  GetValues,
  NewChannel,
  NewTag,
  SaveChannels,
  StartAll,
  StartChannel,
  StopAll,
  StopChannel,
  WriteTag,
  WriteTags
} from '../wailsjs/go/main/App'
import {EventsOn} from '../wailsjs/runtime'

const channels = ref([])
const runtimeMap = ref({})
const valuesMap = ref({})
const logs = ref([])
const activeAlarms = ref([])
const alarmHistory = ref([])
const bottomTab = ref('log')
const compareIds = ref([])
const currentId = ref('')
const clock = ref('')

const currentChannel = computed(() => channels.value.find(c => c.id === currentId.value) || null)
const currentValues = computed(() => valuesMap.value[currentId.value] || {})
const currentRunning = computed(() => runtimeMap.value[currentId.value]?.state === 'Connected')

const channelDialog = ref({visible: false, isNew: false, data: null})
const tagDialog = ref({visible: false, isNew: false, data: null})
const batchDialog = ref({visible: false, tags: []})
const trendDialog = ref({visible: false, tag: null})
const historyDialog = ref({visible: false, tag: null})
const importDialog = ref({visible: false})
const calibDialog = ref({visible: false})

async function refreshRuntime() {
  try {
    const list = await GetRuntime()
    const map = {}
    list.forEach(r => { map[r.id] = r })
    runtimeMap.value = map
  } catch (e) {
    // 刷新失败时保留上一次快照
  }
}

async function refreshValues(id) {
  if (!id) return
  try {
    const vals = await GetValues(id)
    const map = {}
    vals.forEach(v => { map[v.tagId] = v })
    valuesMap.value = {...valuesMap.value, [id]: map}
  } catch (e) {
    // 忽略
  }
}

async function refreshAlarms() {
  try {
    activeAlarms.value = await GetActiveAlarms()
    alarmHistory.value = await GetAlarmHistory(200)
  } catch (e) {
    // 忽略
  }
}

async function loadAll() {
  channels.value = await GetChannels()
  logs.value = await GetLogs()
  await refreshAlarms()
  await refreshRuntime()
  for (const c of channels.value) {
    await refreshValues(c.id)
  }
  if (!currentId.value && channels.value.length > 0) {
    currentId.value = channels.value[0].id
  }
}

async function persist() {
  await SaveChannels(channels.value)
  await refreshRuntime()
}

function onSelect(id) {
  currentId.value = id
  refreshValues(id)
}

async function onStart(id) {
  try {
    await StartChannel(id)
    ElMessage.success('已启动')
  } catch (e) {
    ElMessage.error(String(e))
  }
  await refreshRuntime()
}

async function onStop(id) {
  try {
    await StopChannel(id)
    ElMessage.success('已停止')
  } catch (e) {
    ElMessage.error(String(e))
  }
  await refreshRuntime()
}

async function onStartAll() {
  await StartAll()
  await refreshRuntime()
  ElMessage.success('已启动全部启用通道')
}

async function onStopAll() {
  await StopAll()
  await refreshRuntime()
  ElMessage.success('已停止全部通道')
}

async function onSave() {
  await persist()
  ElMessage.success('配置已保存')
}

async function onAddChannel() {
  const cfg = await NewChannel('通道' + (channels.value.length + 1))
  channelDialog.value = {visible: true, isNew: true, data: cfg}
}

function onEditChannel(channel) {
  channelDialog.value = {visible: true, isNew: false, data: JSON.parse(JSON.stringify(channel))}
}

async function onRemoveChannel(channel) {
  channels.value = channels.value.filter(c => c.id !== channel.id)
  if (currentId.value === channel.id) {
    currentId.value = channels.value.length ? channels.value[0].id : ''
  }
  await persist()
}

async function onChannelSubmit(cfg) {
  if (channelDialog.value.isNew) {
    channels.value = [...channels.value, cfg]
    currentId.value = cfg.id
  } else {
    channels.value = channels.value.map(c => (c.id === cfg.id ? cfg : c))
  }
  await persist()
  await refreshValues(cfg.id)
}

async function onAddTag() {
  const ch = currentChannel.value
  if (!ch) return
  const tag = await NewTag('Tag' + (ch.tags.length + 1))
  tag.slaveId = ch.slaveId
  tagDialog.value = {visible: true, isNew: true, data: tag}
}

function onEditTag(tag) {
  tagDialog.value = {visible: true, isNew: false, data: JSON.parse(JSON.stringify(tag))}
}

async function onRemoveTag(tag) {
  const ch = currentChannel.value
  if (!ch) return
  ch.tags = ch.tags.filter(t => t.id !== tag.id)
  await persist()
  await refreshAlarms()
  // 顺手清掉这个点已落盘的历史，避免删掉的点继续占空间
  try {
    await ClearHistory(tag.id)
  } catch (e) {
    // 历史库未启用或文件不存在，忽略
  }
}

async function onTagSubmit(tag) {
  const ch = currentChannel.value
  if (!ch) return
  if (tagDialog.value.isNew) {
    ch.tags = [...ch.tags, tag]
  } else {
    ch.tags = ch.tags.map(t => (t.id === tag.id ? tag : t))
  }
  await persist()
  await refreshValues(ch.id)
}

async function onWriteTag(tag, value) {
  try {
    await WriteTag(currentId.value, tag.id, value)
    ElMessage.success('写入成功')
    await refreshValues(currentId.value)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function onBatchWrite(tags) {
  batchDialog.value = {visible: true, tags}
}

async function onBatchSubmit(items) {
  try {
    await WriteTags(currentId.value, items)
    ElMessage.success('批量写入成功')
    await refreshValues(currentId.value)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function onTrend(tag) {
  compareIds.value = []
  trendDialog.value = {visible: true, tag}
}

// 对比：把勾选的点位直接作为初始选中项传给曲线对话框
function onCompare(tags) {
  trendDialog.value = {visible: true, tag: tags[0]}
  compareIds.value = tags.map(t => t.id)
}

function onHistory(tag) {
  historyDialog.value = {visible: true, tag}
}

function onImportTags() {
  if (!currentChannel.value) {
    ElMessage.warning('请先选择通道')
    return
  }
  importDialog.value = {visible: true}
}

async function onImported() {
  channels.value = await GetChannels()
  await persist()
  await refreshValues(currentId.value)
}

async function onExportDef() {
  if (!currentId.value) return
  try {
    const path = await ExportTagDefinitions(currentId.value)
    if (path) ElMessage.success(`已导出：${path}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function onAck(id) {
  await AckAlarm(id)
  await refreshAlarms()
}

async function onAckAll() {
  const n = await AckAllAlarms()
  ElMessage.success(n ? `已确认 ${n} 条` : '没有待确认的报警')
  await refreshAlarms()
}

async function onClearAlarmHistory() {
  await ClearAlarmHistory()
  await refreshAlarms()
  ElMessage.success('报警历史已清空')
}

function onCalibration() {
  if (!currentChannel.value) {
    ElMessage.warning('请先选择通道')
    return
  }
  calibDialog.value = {visible: true}
}

// 标定参数写回通道配置并落盘，与点表、通道参数同一份 channels.json
async function onCalibUpdate(patch) {
  const ch = currentChannel.value
  if (!ch) return
  Object.assign(ch, patch)
  await persist()
}

async function onExportCommand(cmd) {
  if (cmd === 'logs') {
    try {
      const path = await ExportLogs()
      if (path) ElMessage.success(`已导出：${path}`)
    } catch (e) {
      ElMessage.error(String(e))
    }
    return
  }
  if (cmd === 'tags') {
    if (!currentId.value) return
    try {
      const path = await ExportTags(currentId.value)
      if (path) ElMessage.success(`已导出：${path}`)
    } catch (e) {
      ElMessage.error(String(e))
    }
    return
  }
  if (cmd === 'tagdef') {
    await onExportDef()
    return
  }
  if (cmd === 'alarms') {
    try {
      const path = await ExportAlarms()
      if (path) ElMessage.success(`已导出：${path}`)
    } catch (e) {
      ElMessage.error(String(e))
    }
  }
}

let timer = 0


onMounted(async () => {
  EventsOn('tag-values', payload => {
    const map = {}
    payload.values.forEach(v => { map[v.tagId] = v })
    valuesMap.value = {...valuesMap.value, [payload.channelId]: map}
  })
  EventsOn('log', entry => {
    logs.value = [...logs.value, entry].slice(-500)
  })
  EventsOn('channel-state', payload => {
    const cur = runtimeMap.value[payload.id] || {}
    runtimeMap.value = {...runtimeMap.value, [payload.id]: {...cur, state: payload.state}}
  })
  EventsOn('alarm', ev => {
    refreshAlarms()
    if (ev && ev.active) {
      bottomTab.value = 'alarm'
    }
  })

  await loadAll()
  timer = window.setInterval(() => {
    refreshRuntime()
    const d = new Date()
    clock.value = d.toLocaleTimeString('zh-CN', {hour12: false})
  }, 1000)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<style scoped>
.app-header {
  height: 44px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  background: #1f3a5f;
  color: #fff;
}

.app-title {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.5px;
}

.app-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.app-clock {
  margin-left: 8px;
  font-family: Consolas, monospace;
  opacity: 0.85;
}

.app-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.bottom-panel {
  height: 250px;
  flex-shrink: 0;
  min-height: 0;
}

/* 标签页内容区要撑满剩余高度，否则里面的表格会塌成 0 高 */
.bottom-tabs {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.bottom-tabs :deep(.el-tabs__header) {
  margin: 0;
  padding: 0 12px;
}

.bottom-tabs :deep(.el-tabs__content) {
  flex: 1;
  min-height: 0;
  padding: 0;
}

.bottom-tabs :deep(.el-tab-pane) {
  height: 100%;
}
</style>
