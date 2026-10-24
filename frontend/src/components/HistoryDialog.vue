<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    width="1020px"
    top="4vh"
    :close-on-click-modal="false"
    @update:model-value="v => emit('update:visible', v)"
    @opened="onOpened"
    @closed="onClosed"
  >
    <div v-if="!settings.enabled" class="hist-banner">
      <span class="banner-icon">!</span>
      <span class="banner-text">
        落库未开启，历史库不会写入任何数据。
        <b>实时曲线走的是内存缓冲，跟这个开关无关</b>，所以曲线照常有数据、历史查询却是空的。
        打开下方「落库」开关后开始记录，此前的曲线数据无法补录。
      </span>
      <el-button size="small" type="primary" plain @click="onEnableRecord">立即开启</el-button>
    </div>

    <div class="hist-toolbar">
      <span class="field-label">点位</span>
      <el-select v-model="tagId" size="small" class="sel-tag" @change="onTagChange">
        <el-option v-for="t in tags" :key="t.id" :label="t.name" :value="t.id" />
      </el-select>

      <el-radio-group v-model="rangeHours" size="small" @change="onRangeChange">
        <el-radio-button v-for="r in HISTORY_RANGES" :key="r.value" :value="r.value">
          {{ r.label }}
        </el-radio-button>
        <el-radio-button :value="-1">自定义</el-radio-button>
      </el-radio-group>

      <template v-if="rangeHours === -1">
        <el-date-picker
          v-model="fromMs"
          type="datetime"
          size="small"
          placeholder="开始时间"
          value-format="x"
          class="picker"
        />
        <span class="tilde">~</span>
        <el-date-picker
          v-model="toMs"
          type="datetime"
          size="small"
          placeholder="结束时间"
          value-format="x"
          class="picker"
        />
      </template>

      <el-button size="small" type="primary" @click="refresh">查询</el-button>
      <el-button size="small" @click="onExport">导出 CSV</el-button>
      <el-button size="small" type="danger" @click="onClearTag">清理该点位</el-button>
    </div>

    <div class="hist-chart-wrap">
      <div ref="chartEl" class="hist-chart"></div>
      <div v-if="emptyHint" class="hist-empty">
        <div class="empty-text">{{ emptyHint }}</div>
      </div>
    </div>

    <div class="hist-stats">
      <div class="stat"><label>查出点数</label><b>{{ stats.count || '--' }}</b></div>
      <div class="stat"><label>最小</label><b>{{ fmt(stats.min) }}</b></div>
      <div class="stat"><label>最大</label><b>{{ fmt(stats.max) }}</b></div>
      <div class="stat"><label>平均</label><b>{{ fmt(stats.avg) }}</b></div>
      <div class="stat"><label>覆盖时间</label><b>{{ stats.span }}</b></div>
      <div class="stat"><label>库中采样</label><b>{{ range.count }}</b></div>
      <div class="stat"><label>该点位占用</label><b>{{ sizeText(range.bytes) }}</b></div>
    </div>

    <div class="hist-settings">
      <span class="field-label">落库</span>
      <el-switch v-model="settings.enabled" size="small" @change="onSaveSettings" />
      <span class="field-label">保留</span>
      <el-input-number
        v-model="settings.retentionDays"
        size="small"
        :min="0"
        :max="3650"
        class="num"
        @change="onSaveSettings"
      />
      <span class="unit">天</span>
      <span class="field-label">上限</span>
      <el-input-number
        v-model="settings.maxSizeMB"
        size="small"
        :min="0"
        :max="102400"
        class="num"
        @change="onSaveSettings"
      />
      <span class="unit">MB</span>
      <el-button size="small" @click="onPrune">立即清理</el-button>
      <el-button size="small" type="danger" @click="onClearAll">清空全部</el-button>
      <span class="lib-stat">
        库内 {{ libStat.tagCount }} 个点位 / {{ libStat.fileCount }} 个分片 / {{ sizeText(libStat.totalBytes) }}
        <template v-if="libStat.oldestDay">（{{ libStat.oldestDay }} ~ {{ libStat.newestDay }}）</template>
      </span>
    </div>
  </el-dialog>
</template>

<script setup>
import {computed, onBeforeUnmount, ref, shallowRef, watch} from 'vue'
import {ElMessage, ElMessageBox} from 'element-plus'
import {HISTORY_RANGES} from '../constants'
import {
  ClearAllHistory,
  ClearHistory,
  ExportHistory,
  GetHistoryRange,
  GetHistorySettings,
  GetHistoryStats,
  PruneHistory,
  QueryHistory,
  SaveHistorySettings
} from '../../wailsjs/go/main/App'

import * as echarts from 'echarts/core'
import {LineChart} from 'echarts/charts'
import {DataZoomComponent, GridComponent, TooltipComponent} from 'echarts/components'
import {CanvasRenderer} from 'echarts/renderers'

echarts.use([LineChart, GridComponent, TooltipComponent, DataZoomComponent, CanvasRenderer])

const props = defineProps({
  visible: {type: Boolean, default: false},
  tag: {type: Object, default: null},
  // 候选点位列表：多通道监视时用合并后的行
  tags: {type: Array, default: () => []}
})
const emit = defineEmits(['update:visible'])

const chartEl = ref(null)
const chart = shallowRef(null)
const tagId = ref('')
const rangeHours = ref(24)
const fromMs = ref(null)
const toMs = ref(null)
const points = ref([])
const stats = ref({count: 0, min: null, max: null, avg: null, span: '--'})
const range = ref({count: 0, bytes: 0, from: 0, to: 0})
const libStat = ref({tagCount: 0, fileCount: 0, totalBytes: 0, oldestDay: '', newestDay: ''})
const settings = ref({enabled: false, retentionDays: 30, maxSizeMB: 512})

let resizeObserver = null

const tags = computed(() => {
  const list = props.tags || []
  if (!list.length && props.tag) return [props.tag]
  const trend = list.filter(t => t.trendEnabled)
  // 没开曲线记录的点查历史也没数据，只在"一个都没有"时才退回兜底，
  // 否则从某个未记录的点进来的话会看不到目标点位
  return trend.length ? trend : list
})

const title = computed(() => {
  const t = tags.value.find(x => x.id === tagId.value)
  return `历史回放 · ${t ? t.name : ''}`
})

// 曲线空着的时侯给一句能照着做的话，而不是让人对着空白图猜。
// 三种情况要分开说：没开落库、点了没开曲线记录、开了但这段时间确实没采到。
const emptyHint = computed(() => {
  if (points.value.length) return ''
  if (!settings.value.enabled) return '落库未开启，暂无历史数据。请在下方打开「落库」开关。'
  const t = tags.value.find(x => x.id === tagId.value)
  if (t && !t.trendEnabled) {
    return `「${t.name}」未启用曲线记录，落库只对同时启用了采集与曲线记录的点位生效。`
  }
  if (!range.value.count) {
    return '库中还没有该点位的任何记录。落库开启后，下一次采集成功即开始写入。'
  }
  return '所选时间范围内没有记录，可切换到「全部」或放宽自定义时间。'
})

watch(() => props.visible, async v => {
  if (!v) return
  if (!tagId.value || !tags.value.some(t => t.id === tagId.value)) {
    tagId.value = props.tag ? props.tag.id : (tags.value[0] ? tags.value[0].id : '')
  }
  settings.value = await GetHistorySettings()
  libStat.value = await GetHistoryStats()
  await loadRange()
  if (chart.value) await refresh()
})

function onTagChange() {
  loadRange()
  if (chart.value) refresh()
}

function onRangeChange() {
  if (chart.value) refresh()
}

function windowMs() {
  const now = Date.now()
  if (rangeHours.value === -1) {
    return [Number(fromMs.value) || 0, Number(toMs.value) || 0]
  }
  if (!rangeHours.value) return [0, 0] // 全部
  return [now - rangeHours.value * 3600 * 1000, now]
}

async function loadRange() {
  if (!tagId.value) return
  try {
    range.value = await GetHistoryRange(tagId.value)
  } catch (e) {
    range.value = {count: 0, bytes: 0, from: 0, to: 0}
  }
}

async function refresh() {
  if (!tagId.value || !chart.value) return
  const [from, to] = windowMs()
  try {
    const pts = await QueryHistory(tagId.value, from, to, 2000)
    points.value = pts || []
    computeStats(points.value)
    chart.value.setOption({series: [{data: points.value.map(p => [p.t, p.v])}]})
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function computeStats(pts) {
  if (!pts.length) {
    stats.value = {count: 0, min: null, max: null, avg: null, span: '--'}
    return
  }
  let min = pts[0].v
  let max = pts[0].v
  let sum = 0
  for (const p of pts) {
    if (p.v < min) min = p.v
    if (p.v > max) max = p.v
    sum += p.v
  }
  stats.value = {
    count: pts.length,
    min,
    max,
    avg: sum / pts.length,
    span: `${stampText(pts[0].t)} ~ ${stampText(pts[pts.length - 1].t)}`
  }
}

async function onExport() {
  if (!tagId.value) return
  const [from, to] = windowMs()
  const t = tags.value.find(x => x.id === tagId.value)
  try {
    const path = await ExportHistory(tagId.value, t ? t.name : tagId.value, from, to)
    if (path) ElMessage.success(`已导出 ${path}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function onClearTag() {
  if (!tagId.value) return
  const t = tags.value.find(x => x.id === tagId.value)
  try {
    await ElMessageBox.confirm(
      `删除「${t ? t.name : tagId.value}」的全部历史数据？删除后无法恢复。`,
      '清理历史',
      {type: 'warning'}
    )
  } catch (e) {
    return
  }
  await ClearHistory(tagId.value)
  points.value = []
  stats.value = {count: 0, min: null, max: null, avg: null, span: '--'}
  if (chart.value) chart.value.setOption({series: [{data: []}]})
  await loadRange()
  libStat.value = await GetHistoryStats()
  ElMessage.success('已清理')
}

// 提示条上的"立即开启"：落库开关生效要等下一个采集周期，这里刷新一下库统计
async function onEnableRecord() {
  settings.value.enabled = true
  await onSaveSettings()
  ElMessage.success('落库已开启，下一个采集周期开始记录')
}

async function onPrune() {
  const res = await PruneHistory()
  const files = res ? res.files : 0
  ElMessage.success(files ? `已清理 ${files} 个分片` : '没有需要清理的分片')
  libStat.value = await GetHistoryStats()
  await loadRange()
}

async function onClearAll() {
  try {
    await ElMessageBox.confirm('清空全部点位的历史数据？该操作不可恢复。', '清空历史库', {type: 'warning'})
  } catch (e) {
    return
  }
  await ClearAllHistory()
  points.value = []
  if (chart.value) chart.value.setOption({series: [{data: []}]})
  libStat.value = await GetHistoryStats()
  await loadRange()
  ElMessage.success('历史库已清空')
}

async function onSaveSettings() {
  await SaveHistorySettings(settings.value)
  libStat.value = await GetHistoryStats()
}

function buildOption() {
  return {
    animation: false,
    grid: {left: 66, right: 22, top: 20, bottom: 52},
    tooltip: {
      trigger: 'axis',
      axisPointer: {type: 'cross'},
      formatter: params => {
        if (!params || !params.length) return ''
        const p = params[0]
        return `${stampText(p.value[0])}<br/><b>${fmt(p.value[1])}</b>`
      }
    },
    xAxis: {
      type: 'time',
      axisLabel: {formatter: '{HH}:{mm}'},
      splitLine: {show: true, lineStyle: {type: 'dashed', opacity: 0.35}}
    },
    yAxis: {
      type: 'value',
      scale: true,
      splitLine: {lineStyle: {type: 'dashed', opacity: 0.35}}
    },
    dataZoom: [
      {type: 'inside', filterMode: 'none'},
      {type: 'slider', height: 18, bottom: 14}
    ],
    series: [{
      type: 'line',
      showSymbol: false,
      sampling: 'lttb',
      lineStyle: {width: 1.5, color: '#2f6fd0'},
      itemStyle: {color: '#2f6fd0'},
      areaStyle: {color: 'rgba(47,111,208,0.10)'},
      data: []
    }]
  }
}

async function onOpened() {
  if (chart.value || !chartEl.value) return
  chart.value = echarts.init(chartEl.value)
  chart.value.setOption(buildOption())
  resizeObserver = new ResizeObserver(() => {
    if (chart.value) chart.value.resize()
  })
  resizeObserver.observe(chartEl.value)
  await refresh()
}

function onClosed() {
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (chart.value) {
    chart.value.dispose()
    chart.value = null
  }
}

onBeforeUnmount(() => {
  if (resizeObserver) resizeObserver.disconnect()
  if (chart.value) chart.value.dispose()
})

function fmt(v) {
  if (v === null || v === undefined || Number.isNaN(v)) return '--'
  const a = Math.abs(v)
  if (a === 0) return '0'
  const s = a >= 1000 ? v.toFixed(2) : a >= 1 ? v.toFixed(4) : v.toPrecision(6)
  return String(Number(s))
}

function sizeText(bytes) {
  const b = Number(bytes) || 0
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}

function stampText(ms) {
  if (!ms) return '--'
  const d = new Date(ms)
  const p = n => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
</script>

<style scoped>
.hist-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.field-label {
  font-size: 12px;
  color: #606266;
}

.sel-tag {
  width: 150px;
}

.picker {
  width: 175px;
}

.tilde {
  color: #909399;
}

.hist-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
  padding: 8px 12px;
  background: #fdf6ec;
  border: 1px solid #f5dab1;
  border-left: 3px solid #e6a23c;
  border-radius: 4px;
}

.banner-icon {
  flex: none;
  width: 18px;
  height: 18px;
  line-height: 18px;
  text-align: center;
  border-radius: 50%;
  background: #e6a23c;
  color: #fff;
  font-size: 12px;
  font-weight: 700;
}

.banner-text {
  font-size: 12px;
  line-height: 1.7;
  color: #8a6d3b;
}

.hist-chart-wrap {
  position: relative;
}

.hist-chart {
  width: 100%;
  height: 320px;
  border: 1px solid #d7dee8;
  border-radius: 4px;
  background: #fff;
}

/* 空状态浮在图上但不吃鼠标事件，免得挡住下面的 dataZoom 拖动 */
.hist-empty {
  position: absolute;
  left: 0;
  right: 0;
  top: 0;
  bottom: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}

.empty-text {
  max-width: 78%;
  padding: 10px 16px;
  text-align: center;
  font-size: 13px;
  line-height: 1.7;
  color: #7a869a;
  background: rgba(244, 247, 251, 0.92);
  border: 1px dashed #c3cede;
  border-radius: 4px;
}

.hist-stats {
  display: flex;
  gap: 18px;
  margin-top: 10px;
  padding: 8px 12px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  flex-wrap: wrap;
}

.stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.stat label {
  font-size: 12px;
  color: #7a869a;
}

.stat b {
  font-family: Consolas, monospace;
  font-size: 13px;
  color: #24303f;
}

.hist-settings {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  flex-wrap: wrap;
}

.num {
  width: 110px;
}

.unit {
  font-size: 12px;
  color: #909399;
  margin-left: -4px;
}

.lib-stat {
  margin-left: auto;
  font-size: 12px;
  color: #7a869a;
  font-family: Consolas, monospace;
}
</style>
