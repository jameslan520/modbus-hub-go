<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    width="1000px"
    top="4vh"
    :close-on-click-modal="false"
    @update:model-value="v => emit('update:visible', v)"
    @opened="onOpened"
    @closed="onClosed"
  >
    <div class="trend-toolbar">
      <span class="field-label">点位</span>
      <el-select
        v-model="selected"
        multiple
        collapse-tags
        collapse-tags-tooltip
        size="small"
        class="sel-tags"
        :max-collapse-tags="3"
        @change="onSelectionChange"
      >
        <el-option v-for="t in options" :key="t.id" :label="t.name" :value="t.id" />
      </el-select>

      <el-radio-group v-model="windowSec" size="small" @change="onWindowChange">
        <el-radio-button v-for="w in TREND_WINDOWS" :key="w.value" :value="w.value">
          {{ w.label }}
        </el-radio-button>
      </el-radio-group>

      <el-button size="small" :type="paused ? 'danger' : 'default'" @click="togglePause">
        {{ paused ? '继续刷新' : '暂停刷新' }}
      </el-button>
      <el-button size="small" :disabled="selected.length !== 1" @click="onExport">导出 CSV</el-button>
      <el-button size="small" :disabled="!selected.length" @click="onClear">清空曲线</el-button>
    </div>

    <div class="trend-toolbar second">
      <el-checkbox v-model="dualAxis" size="small" @change="refresh">双 Y 轴</el-checkbox>
      <el-select
        v-if="dualAxis"
        v-model="rightAxisTag"
        size="small"
        class="sel-axis"
        @change="refresh"
      >
        <el-option v-for="t in selectedTags" :key="t.id" :label="t.name + '（右轴）'" :value="t.id" />
      </el-select>
      <el-checkbox v-model="showLimits" size="small" @change="refresh">显示报警阈值</el-checkbox>
      <span class="trend-hint">
        {{ hintText }}
      </span>
    </div>

    <div ref="chartEl" class="trend-chart"></div>

    <div class="trend-stats">
      <div v-for="row in statRows" :key="row.id" class="stat-row">
        <span class="dot" :style="{background: row.color}"></span>
        <span class="row-name" :title="row.name">{{ row.name }}</span>
        <span class="row-cur">{{ row.current }}</span>
        <span class="row-item">最小 {{ fmt(row.min) }}</span>
        <span class="row-item">最大 {{ fmt(row.max) }}</span>
        <span class="row-item">平均 {{ fmt(row.avg) }}</span>
        <span class="row-item">末值 {{ fmt(row.last) }}</span>
        <span class="row-item">{{ row.count }} 点</span>
      </div>
      <el-empty v-if="!statRows.length" description="勾选点位后显示统计" :image-size="50" />
    </div>
  </el-dialog>
</template>

<script setup>
import {computed, onBeforeUnmount, ref, shallowRef, watch} from 'vue'
import {ElMessage, ElMessageBox} from 'element-plus'
import {SERIES_COLORS, TREND_WINDOWS} from '../constants'
import {ClearTrend, ExportTrend, GetTrends, GetTrendStats} from '../../wailsjs/go/main/App'

import * as echarts from 'echarts/core'
import {LineChart} from 'echarts/charts'
import {
  DataZoomComponent,
  GridComponent,
  LegendComponent,
  MarkLineComponent,
  TooltipComponent
} from 'echarts/components'
import {CanvasRenderer} from 'echarts/renderers'

echarts.use([
  LineChart, GridComponent, TooltipComponent, DataZoomComponent,
  LegendComponent, MarkLineComponent, CanvasRenderer
])

const props = defineProps({
  visible: {type: Boolean, default: false},
  tag: {type: Object, default: null},
  // 点表传进来才能选多个点位做对比
  channel: {type: Object, default: null},
  // 点表勾选后点"对比曲线"传进来的初始选中项
  compareIds: {type: Array, default: () => []},
  values: {type: Object, default: () => ({})}
})
const emit = defineEmits(['update:visible'])

const chartEl = ref(null)
const chart = shallowRef(null)
const windowSec = ref(300)
const paused = ref(false)
const selected = ref([])
const dualAxis = ref(false)
const rightAxisTag = ref('')
const showLimits = ref(true)
const series = ref([])

let timer = 0
let resizeObserver = null

const options = computed(() => {
  const list = props.channel && props.channel.tags ? props.channel.tags : []
  const trend = list.filter(t => t.trendEnabled)
  if (!trend.length && props.tag) return [props.tag]
  return trend
})

const selectedTags = computed(() => options.value.filter(t => selected.value.includes(t.id)))

const title = computed(() => {
  if (selectedTags.value.length === 1) return `点位曲线 · ${selectedTags.value[0].name}`
  return `曲线对比 · ${selectedTags.value.length} 个点位`
})

const hintText = computed(() => {
  if (!series.value.length) return '暂无历史数据，先启动通道采集'
  const total = series.value.reduce((n, s) => n + (s.points ? s.points.length : 0), 0)
  return `${series.value.length} 条曲线 · 共 ${total} 点 · 缓冲上限 16384/点`
})

const statRows = computed(() => {
  return series.value.map((s, i) => {
    const pts = s.points || []
    let min = null
    let max = null
    let sum = 0
    for (const p of pts) {
      if (min === null || p.v < min) min = p.v
      if (max === null || p.v > max) max = p.v
      sum += p.v
    }
    const v = props.values[s.tagId]
    return {
      id: s.tagId,
      name: s.name,
      color: SERIES_COLORS[i % SERIES_COLORS.length],
      current: v && v.displayValue ? v.displayValue : '--',
      min,
      max,
      avg: pts.length ? sum / pts.length : null,
      last: pts.length ? pts[pts.length - 1].v : null,
      count: pts.length
    }
  })
})

watch(() => props.visible, v => {
  if (!v) return
  // 打开时：对比优先用点表勾选的点位，单点查看用传入的点位
  if (props.compareIds && props.compareIds.length) {
    selected.value = props.compareIds.filter(id => options.value.some(t => t.id === id))
  } else if (props.tag) {
    selected.value = [props.tag.id]
  }
  if (!selected.value.length && options.value.length) {
    selected.value = [options.value[0].id]
  }
  if (dualAxis.value && !selected.value.includes(rightAxisTag.value)) {
    rightAxisTag.value = selected.value[1] || selected.value[0]
  }
})

function fmt(v) {
  if (v === null || v === undefined || Number.isNaN(v)) return '--'
  const a = Math.abs(v)
  if (a === 0) return '0'
  const s = a >= 1000 ? v.toFixed(2) : a >= 1 ? v.toFixed(4) : v.toPrecision(6)
  return String(Number(s))
}

function onSelectionChange() {
  if (dualAxis.value && !selected.value.includes(rightAxisTag.value)) {
    rightAxisTag.value = selected.value[1] || selected.value[0] || ''
  }
  refresh()
}

function onWindowChange() {
  if (paused.value) refresh()
}

function togglePause() {
  paused.value = !paused.value
  if (!paused.value) refresh()
}

async function refresh() {
  if (!selected.value.length) {
    series.value = []
    if (chart.value) chart.value.setOption({series: []}, {replaceMerge: ['series']})
    return
  }
  try {
    const list = await GetTrends(selected.value, windowSec.value, 1500)
    series.value = list || []
    if (chart.value) {
      chart.value.setOption({
        yAxis: buildYAxis(),
        legend: {data: series.value.map(s => s.name)},
        series: buildSeries()
      }, {replaceMerge: ['series', 'yAxis']})
    }
  } catch (e) {
    // 轮询刷新失败不打断界面，下一拍继续
  }
}

function buildYAxis() {
  const left = {
    type: 'value',
    scale: true,
    position: 'left',
    splitLine: {lineStyle: {type: 'dashed', opacity: 0.35}}
  }
  if (!dualAxis.value) return [left]
  return [
    left,
    {
      type: 'value',
      scale: true,
      position: 'right',
      splitLine: {show: false}
    }
  ]
}

function buildSeries() {
  return series.value.map((s, i) => {
    const color = SERIES_COLORS[i % SERIES_COLORS.length]
    const axisIndex = dualAxis.value && s.tagId === rightAxisTag.value ? 1 : 0
    const markLine = {silent: true, symbol: 'none', data: []}
    if (showLimits.value) {
      if (s.high !== null && s.high !== undefined) {
        markLine.data.push({
          yAxis: s.high,
          lineStyle: {color, type: 'dashed', width: 1},
          label: {formatter: `${s.name} 上限 ${s.high}`, fontSize: 10, position: 'insideEndTop'}
        })
      }
      if (s.low !== null && s.low !== undefined) {
        markLine.data.push({
          yAxis: s.low,
          lineStyle: {color, type: 'dashed', width: 1},
          label: {formatter: `${s.name} 下限 ${s.low}`, fontSize: 10, position: 'insideEndBottom'}
        })
      }
    }
    return {
      name: s.name,
      type: 'line',
      yAxisIndex: axisIndex,
      showSymbol: false,
      sampling: 'lttb',
      lineStyle: {width: 1.6, color},
      itemStyle: {color},
      areaStyle: series.value.length === 1 ? {color: 'rgba(47,111,208,0.12)'} : undefined,
      markLine: markLine.data.length ? markLine : undefined,
      data: (s.points || []).map(p => [p.t, p.v])
    }
  })
}

function buildOption() {
  return {
    animation: false,
    grid: {left: 66, right: 66, top: 34, bottom: 46},
    legend: {top: 2, itemHeight: 8, itemWidth: 14, textStyle: {fontSize: 11}},
    tooltip: {
      trigger: 'axis',
      axisPointer: {type: 'cross'},
      formatter: params => {
        if (!params || !params.length) return ''
        const time = new Date(params[0].value[0])
          .toLocaleTimeString('zh-CN', {hour12: false})
        let html = time
        for (const p of params) {
          const unit = unitOf(p.seriesName)
          html += `<br/>${p.marker}${p.seriesName} <b>${fmt(p.value[1])}</b>${unit}`
        }
        return html
      }
    },
    xAxis: {
      type: 'time',
      axisLabel: {formatter: '{HH}:{mm}:{ss}'},
      splitLine: {show: true, lineStyle: {type: 'dashed', opacity: 0.35}}
    },
    yAxis: buildYAxis(),
    dataZoom: [{type: 'inside', filterMode: 'none'}],
    series: []
  }
}

function unitOf(name) {
  const t = options.value.find(x => x.name === name)
  return t && t.unit ? ' ' + t.unit : ''
}

async function onClear() {
  if (!selected.value.length) return
  try {
    await ElMessageBox.confirm(
      `清空这 ${selected.value.length} 个点位的全部历史曲线？清空后无法恢复。`,
      '清空曲线',
      {type: 'warning'}
    )
  } catch (e) {
    return
  }
  for (const id of selected.value) {
    await ClearTrend(id)
  }
  series.value = []
  if (chart.value) chart.value.setOption({series: []}, {replaceMerge: ['series']})
  ElMessage.success('已清空')
}

async function onExport() {
  const id = selected.value[0]
  if (!id) return
  const t = options.value.find(x => x.id === id)
  try {
    const path = await ExportTrend(id, t ? t.name : id, windowSec.value)
    if (!path) return
    ElMessage.success(`已导出 ${path}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function onOpened() {
  if (chart.value || !chartEl.value) return
  chart.value = echarts.init(chartEl.value)
  chart.value.setOption(buildOption())

  resizeObserver = new ResizeObserver(() => {
    if (chart.value) chart.value.resize()
  })
  resizeObserver.observe(chartEl.value)

  refresh()
  timer = window.setInterval(() => {
    if (!paused.value) refresh()
  }, 500)
}

function onClosed() {
  if (timer) {
    window.clearInterval(timer)
    timer = 0
  }
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (chart.value) {
    chart.value.dispose()
    chart.value = null
  }
  series.value = []
}

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  if (resizeObserver) resizeObserver.disconnect()
  if (chart.value) chart.value.dispose()
})
</script>

<style scoped>
.trend-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.trend-toolbar.second {
  margin-bottom: 8px;
}

.field-label {
  font-size: 12px;
  color: #606266;
}

.sel-tags {
  width: 260px;
}

.sel-axis {
  width: 170px;
}

.trend-hint {
  margin-left: auto;
  font-size: 12px;
  color: #7a869a;
  font-family: Consolas, monospace;
}

.trend-chart {
  width: 100%;
  height: 320px;
  border: 1px solid #d7dee8;
  border-radius: 4px;
  background: #fff;
}

.trend-stats {
  margin-top: 10px;
  padding: 8px 12px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  max-height: 132px;
  overflow: auto;
}

.stat-row {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
  line-height: 22px;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.row-name {
  width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #24303f;
}

.row-cur {
  width: 90px;
  font-family: Consolas, monospace;
  font-weight: 600;
  color: #1f7a3d;
}

.row-item {
  font-family: Consolas, monospace;
  color: #606266;
}
</style>
