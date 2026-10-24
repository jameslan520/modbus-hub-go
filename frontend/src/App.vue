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
            <el-dropdown-item command="tags" :disabled="!watchedIds.length">当前点表</el-dropdown-item>
            <el-dropdown-item command="tagdef" :disabled="!watchedIds.length">点位定义（可再导入）</el-dropdown-item>
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
        :watched-ids="watchedIds"
        @select="onSelect"
        @watch="onWatch"
        @add="onAddChannel"
        @edit="onEditChannel"
        @remove="onRemoveChannel"
        @start="onStart"
        @stop="onStop"
      />

      <div class="app-main">
        <TagTable
          :rows="tagRows"
          :primary="currentChannel"
          :multi="multiWatch"
          :values="mergedValues"
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
      :channel="dialogChannel"
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
      :tags="tagRows"
    />
    <BatchWriteDialog
      v-model:visible="batchDialog.visible"
      :tags="batchDialog.tags"
      @submit="onBatchSubmit"
    />
    <WriteDialog
      v-model:visible="writeDialog.visible"
      :tag="writeDialog.tag"
      :channel-id="writeDialog.channelId"
      @submit="onWriteSubmit"
    />
    <TrendDialog
      v-model:visible="trendDialog.visible"
      :tag="trendDialog.tag"
      :tags="trendOptions"
      :compare-ids="compareIds"
      :values="mergedValues"
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
import WriteDialog from './components/WriteDialog.vue'
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
// watchedIds 当前正在监视的通道（点表里同时显示它们的点）。
// currentId 仍是"主通道"，新增点 / 导入 / 导出 / 标定这类只能针对单个通道的操作以它为准。
const watchedIds = ref([])
const compareIds = ref([])
const currentId = ref('')
const clock = ref('')

const currentChannel = computed(() => channels.value.find(c => c.id === currentId.value) || null)
const currentRunning = computed(() => runtimeMap.value[currentId.value]?.state === 'Connected')
// 点参数对话框里的联动写入候选项，要取自点位所属通道
const dialogChannel = computed(() => channelOf(tagDialog.value.channelId) || currentChannel.value)

// 按通道列表的原始顺序取，避免勾选顺序影响点表行的排列
const watchedChannels = computed(() => channels.value.filter(c => watchedIds.value.includes(c.id)))
const multiWatch = computed(() => watchedChannels.value.length > 1)

// 合并后的点表行。带上所属通道，后续编辑/删除/写入才能路由回去。
// 上面两个字段只存在于这份视图行里，是给界面认通道用的，不能写回配置
const tagRows = computed(() => {
  const out = []
  for (const c of watchedChannels.value) {
    for (const t of c.tags || []) {
      out.push({...t, channelId: c.id, channelName: c.name})
    }
  }
  return out
})

// 多通道的值是一份 map per channel，这里摊平成 tagId -> value
const mergedValues = computed(() => {
  const out = {}
  for (const id of watchedIds.value) {
    Object.assign(out, valuesMap.value[id] || {})
  }
  return out
})

// 标定只针对主通道，单独取它自己的值，不要把别的通道混进来
const currentValues = computed(() => valuesMap.value[currentId.value] || {})

// 曲线对比的候选项：跨通道也要能选，所以不看单个 channel
const trendOptions = computed(() => tagRows.value.filter(t => t.trendEnabled))

// 把视图行还原成纯点位：channelId / channelName 是界面附加上去的
function stripRow(row) {
  const t = JSON.parse(JSON.stringify(row))
  delete t.channelId
  delete t.channelName
  return t
}

function channelOf(id) {
  return channels.value.find(c => c.id === id) || null
}

const channelDialog = ref({visible: false, isNew: false, data: null})
const tagDialog = ref({visible: false, isNew: false, data: null})
const batchDialog = ref({visible: false, tags: []})
const writeDialog = ref({visible: false, tag: null, channelId: ''})
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
  if (!watchedIds.value.length && currentId.value) {
    watchedIds.value = [currentId.value]
  }
  // 配置被外部改动过（比如删了通道）时监视集合要跟着收敛
  const alive = channels.value.map(c => c.id)
  watchedIds.value = watchedIds.value.filter(id => alive.includes(id))
  if (!watchedIds.value.length && currentId.value) {
    watchedIds.value = [currentId.value]
  }
}

async function persist() {
  await SaveChannels(channels.value)
  await refreshRuntime()
}

// 点通道行 = 只监视它（保持原来的单选习惯）
function onSelect(id) {
  currentId.value = id
  watchedIds.value = [id]
  refreshValues(id)
}

// 勾复选框 = 把通道并入/移出监视集合。至少留一个，全空了就把主通道放回来。
function onWatch(id, on) {
  if (on) {
    if (!watchedIds.value.includes(id)) watchedIds.value = [...watchedIds.value, id]
    refreshValues(id)
    return
  }
  const next = watchedIds.value.filter(x => x !== id)
  watchedIds.value = next.length ? next : [id]
  // 主通道被移出监视集合时，改指到第一个还在监视的，避免按钮作用在一个没显示的通道上
  if (!watchedIds.value.includes(currentId.value)) {
    currentId.value = watchedIds.value[0] || ''
  }
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
  watchedIds.value = watchedIds.value.filter(id => id !== channel.id)
  if (currentId.value === channel.id) {
    currentId.value = channels.value.length ? channels.value[0].id : ''
  }
  if (!watchedIds.value.length && currentId.value) {
    watchedIds.value = [currentId.value]
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
  tagDialog.value = {visible: true, isNew: true, data: tag, channelId: ch.id}
}

// 编辑多点位弹出时用该点位所属通道：联动写入的候选项必须是同一通道里的可写点
function onEditTag(row) {
  tagDialog.value = {
    visible: true,
    isNew: false,
    data: stripRow(row),
    channelId: row.channelId || currentId.value
  }
}

async function onRemoveTag(row) {
  const ch = channelOf(row.channelId) || currentChannel.value
  if (!ch) return
  ch.tags = ch.tags.filter(t => t.id !== row.id)
  await persist()
  await refreshAlarms()
  // 顺手清掉这个点已落盘的历史，避免删掉的点继续占空间
  try {
    await ClearHistory(row.id)
  } catch (e) {
    // 历史库未启用或文件不存在，忽略
  }
}

async function onTagSubmit(tag) {
  const ch = channelOf(tagDialog.value.channelId) || currentChannel.value
  if (!ch) return
  // 兜底：视图行上的附加字段绝不能进配置
  const clean = stripRow(tag)
  clean.alarmActionTag = clean.alarmActionTag || ''
  if (tagDialog.value.isNew) {
    ch.tags = [...ch.tags, clean]
  } else {
    ch.tags = ch.tags.map(t => (t.id === clean.id ? clean : t))
  }
  await persist()
  await refreshValues(ch.id)
}

// 点表点「写入」只负责开对话框，具体值由对话框收，收完再回到这里下发
function onWriteTag(row) {
  writeDialog.value = {
    visible: true,
    tag: row,
    channelId: row.channelId || currentId.value
  }
}

async function onWriteSubmit(item) {
  const channelID = writeDialog.value.channelId
  try {
    await WriteTag(channelID, item.tagId, item.value, item.mode)
    ElMessage.success('写入成功')
    await refreshValues(channelID)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function onBatchWrite(tags) {
  batchDialog.value = {visible: true, tags}
}

async function onBatchSubmit(items) {
  // 同一次请求不能跨通道，按所属通道拆开各写一次
  const groups = {}
  for (const it of items) {
    const cid = it.channelId || currentId.value
    if (!groups[cid]) groups[cid] = []
    groups[cid].push({tagId: it.tagId, value: it.value})
  }
  let n = 0
  for (const cid of Object.keys(groups)) {
    await WriteTags(cid, groups[cid])
    n += groups[cid].length
    await refreshValues(cid)
  }
  ElMessage.success(`批量写入成功，共 ${n} 个点`)
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
  for (const id of watchedIds.value) {
    await refreshValues(id)
  }
}

async function onExportDef() {
  if (!watchedIds.value.length) return
  try {
    const path = await ExportTagDefinitions(watchedIds.value)
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
    if (!watchedIds.value.length) return
    try {
      const path = await ExportTags(watchedIds.value)
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
