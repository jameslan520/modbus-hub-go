<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    width="1000px"
    top="4vh"
    :close-on-click-modal="false"
    @update:model-value="v => emit('update:visible', v)"
    @opened="onOpened"
  >
    <div class="calib-top">
      <span class="field">
        <label>实测点位</label>
        <el-select v-model="tagId" size="small" placeholder="选择点位" @change="onTagChange">
          <el-option v-for="t in enabledTags" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
      </span>
      <span class="field">
        <label>采样点数</label>
        <el-select v-model="sampleCount" size="small" @change="onParamChange">
          <el-option v-for="n in CALIB_SAMPLE_COUNTS" :key="n" :label="`${n} 点`" :value="n" />
        </el-select>
      </span>
      <span class="field">
        <label>起始地址</label>
        <el-input-number v-model="startAddress" size="small" :min="0" :max="65535" :controls="false"
          style="width: 110px" @change="onParamChange" />
      </span>
      <span class="field">
        <label>下载类型</label>
        <el-select v-model="dataType" size="small" @change="onParamChange">
          <el-option v-for="d in DATA_TYPES" :key="d.value" :label="d.label" :value="d.value" />
        </el-select>
      </span>
      <span class="field">
        <label>写入模式</label>
        <el-select v-model="mode" size="small" @change="onParamChange">
          <el-option v-for="m in CALIB_WRITE_MODES" :key="m.value" :label="m.label" :value="m.value" />
        </el-select>
      </span>
      <span v-if="orderVisible" class="field">
        <label>字节顺序</label>
        <el-select v-model="byteOrder" size="small" @change="onParamChange">
          <el-option v-for="o in BYTE_ORDERS" :key="o.value" :label="o.label" :value="o.value" />
        </el-select>
      </span>
      <span class="calib-state" :class="running ? 'ok' : 'bad'">
        {{ running ? '通道已连接' : '通道未运行' }}
      </span>
    </div>

    <el-row :gutter="12" class="calib-body">
      <el-col :span="10">
        <div class="block">
          <div class="block-title">实时采样</div>
          <div class="live">
            <div class="live-item">
              <label>当前值</label><b class="live-now">{{ currentText }}</b>
            </div>
            <div class="live-item">
              <label>{{ sampleHint }}</label><b class="live-avg">{{ averageText }}</b>
            </div>
          </div>

          <el-table :data="samples" size="small" height="176" border>
            <el-table-column type="index" label="#" width="44" />
            <el-table-column label="时间" width="90">
              <template #default="{row}">{{ clockText(row.t) }}</template>
            </el-table-column>
            <el-table-column label="实测值">
              <template #default="{row}">{{ fmt(row.v) }}</template>
            </el-table-column>
          </el-table>

          <div class="add-row">
            <el-input-number v-model="standard" size="small" :controls="false" placeholder="标准值"
              style="width: 130px" />
            <el-button size="small" type="primary" @click="onAddPoint">添加标定点</el-button>
          </div>
          <p class="tip">先等采集稳定，填入标准值后点击添加；均值取上表最近 {{ sampleCount }} 点。</p>
        </div>
      </el-col>

      <el-col :span="14">
        <div class="block">
          <div class="block-title">
            <span>标定点</span>
            <el-button size="small" type="danger" :disabled="!points.length" @click="onClearPoints">清空</el-button>
          </div>

          <el-table :data="points" size="small" height="240" border>
            <el-table-column type="index" label="#" width="44" />
            <el-table-column label="实测值 (x)">
              <template #default="{row}">{{ fmt(row.measured) }}</template>
            </el-table-column>
            <el-table-column label="标准值 (y)">
              <template #default="{row}">{{ fmt(row.standard) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="80" fixed="right">
              <template #default="{row, $index}">
                <el-button size="small" type="danger" @click="onRemovePoint($index)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="fit-box">
            <div class="fit-item">
              <label>斜率 k</label><b>{{ fit.slopeText || '--' }}</b>
            </div>
            <div class="fit-item">
              <label>截距 b</label><b>{{ fit.interceptText || '--' }}</b>
            </div>
            <div class="fit-hint">{{ fit.hint || '至少 2 个标定点后显示拟合' }}</div>
          </div>
        </div>
      </el-col>
    </el-row>

    <template #footer>
      <div class="calib-footer">
        <span class="footer-info">{{ downloadSummary }}</span>
        <el-button size="small" @click="emit('update:visible', false)">关闭</el-button>
        <el-button size="small" type="primary" :loading="downloading" @click="onDownload">下载标定</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup>
import {computed, ref, watch} from 'vue'
import {ElMessage, ElMessageBox} from 'element-plus'
import {
  BYTE_ORDERS,
  CALIB_SAMPLE_COUNTS,
  CALIB_WRITE_MODES,
  DATA_TYPES,
  byteOrderApplies,
  clockText
} from '../constants'
import {
  DownloadCalibration,
  FitCalibration,
  GetRecentSamples
} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime'

const props = defineProps({
  visible: {type: Boolean, default: false},
  channel: {type: Object, default: null},
  values: {type: Object, default: () => ({})},
  running: {type: Boolean, default: false}
})
const emit = defineEmits(['update:visible', 'update'])

const tagId = ref('')
const sampleCount = ref(8)
const startAddress = ref(0)
const dataType = ref(1)
const mode = ref(0)
const byteOrder = ref(0)
const points = ref([])
const samples = ref([])
const standard = ref(null)

// UInt16 只有一个寄存器，没有字序问题，切到 Float32/Int32/Double64 才显示下拉
const orderVisible = computed(() => byteOrderApplies(dataType.value))
const fit = ref({ok: false, slopeText: '--', interceptText: '--', hint: ''})
const downloading = ref(false)

let lastStamp = 0
let lastTagId = ''

const title = computed(() => `传感器标定 · ${props.channel ? props.channel.name : ''}`)
const enabledTags = computed(() => (props.channel ? props.channel.tags.filter(t => t.isEnabled) : []))

const currentText = computed(() => {
  if (!tagId.value) return '--'
  const v = props.values[tagId.value]
  return v && v.displayValue ? v.displayValue : '--'
})

const average = computed(() => {
  if (!samples.value.length) return null
  const sum = samples.value.reduce((acc, s) => acc + s.v, 0)
  return sum / samples.value.length
})

const averageText = computed(() => (average.value === null ? '--' : fmt(average.value)))

const sampleHint = computed(() => {
  const n = samples.value.length
  if (!n) return '等待有效采集'
  return n >= sampleCount.value
    ? `下列 ${sampleCount.value} 点均值`
    : `已采集 ${n}/${sampleCount.value} 点`
})

// 该数据类型下单个值占的寄存器数
const regCount = computed(() => {
  if (dataType.value === 1 || dataType.value === 3) return 2
  if (dataType.value === 2) return 4
  return 1
})

const downloadRegisters = computed(() => {
  if (mode.value === 1) return 2 * regCount.value
  return points.value.length * 2 * regCount.value
})

const downloadSummary = computed(() => {
  if (!downloadRegisters.value) return '请先添加标定点'
  const addr = Number(startAddress.value) || 0
  return `功能码 16 写入从机 ${props.channel ? props.channel.slaveId : '-'}，` +
    `起始地址 ${addr}，共 ${downloadRegisters.value} 个寄存器`
})

function fmt(v) {
  if (v === null || v === undefined || Number.isNaN(v)) return '--'
  const a = Math.abs(v)
  if (a === 0) return '0'
  const s = a >= 1000 ? v.toFixed(2) : a >= 1 ? v.toFixed(4) : v.toPrecision(6)
  return String(Number(s))
}

// 打开对话框时从通道配置恢复标定参数
function loadFromChannel() {
  const c = props.channel
  if (!c) return
  sampleCount.value = CALIB_SAMPLE_COUNTS.includes(c.calibrationSampleCount) ? c.calibrationSampleCount : 8
  startAddress.value = Number(c.calibrationStartAddress) || 0
  dataType.value = DATA_TYPES.some(d => d.value === c.calibrationDataType) ? c.calibrationDataType : 1
  mode.value = c.calibrationWriteMode === 1 ? 1 : 0
  byteOrder.value = Number.isFinite(Number(c.calibrationByteOrder)) ? Number(c.calibrationByteOrder) : 0
  points.value = (c.calibrationPoints || []).map(p => ({
    measured: Number(p.measured) || 0,
    standard: Number(p.standard) || 0
  }))
  tagId.value = c.calibrationTagId && enabledTags.value.some(t => t.id === c.calibrationTagId)
    ? c.calibrationTagId
    : (enabledTags.value.length ? enabledTags.value[0].id : '')
  updateFit()
}

// 用后端缓冲里的历史采样做种子，避免打开对话框后要重新等一轮采集
async function seedSamples() {
  samples.value = []
  lastStamp = 0
  lastTagId = ''
  if (!tagId.value) return
  try {
    const pts = await GetRecentSamples(tagId.value, sampleCount.value)
    samples.value = (pts || []).map(p => ({t: p.t, v: p.v}))
    if (samples.value.length) {
      lastTagId = tagId.value
      lastStamp = samples.value[samples.value.length - 1].t
    }
  } catch (e) {
    // 无历史数据时等实时采集即可
  }
}

async function updateFit() {
  if (!points.value.length) {
    fit.value = {ok: false, slopeText: '--', interceptText: '--', hint: '至少 2 个标定点后显示拟合'}
    return
  }
  try {
    fit.value = await FitCalibration(points.value)
  } catch (e) {
    fit.value = {ok: false, slopeText: '--', interceptText: '--', hint: '拟合失败'}
  }
}

function emitUpdate() {
  emit('update', {
    calibrationTagId: tagId.value,
    calibrationSampleCount: sampleCount.value,
    calibrationStartAddress: Number(startAddress.value) || 0,
    calibrationDataType: dataType.value,
    calibrationByteOrder: byteOrder.value,
    calibrationWriteMode: mode.value,
    calibrationPoints: points.value.map(p => ({
      measured: p.measured,
      standard: p.standard
    }))
  })
}

function trimSamples() {
  const keep = Math.max(1, sampleCount.value)
  while (samples.value.length > keep) samples.value.shift()
}

watch(() => props.visible, async v => {
  if (!v) return
  loadFromChannel()
  await seedSamples()
})

watch(() => props.channel && props.channel.id, () => {
  if (props.visible) loadFromChannel()
})

async function onTagChange() {
  await seedSamples()
  emitUpdate()
}

function onParamChange() {
  trimSamples()
  emitUpdate()
}

async function onAddPoint() {
  if (average.value === null) {
    ElMessage.warning('还没有有效的实测均值。请先启动通道，等采集稳定后再添加。')
    return
  }
  if (standard.value === null || standard.value === undefined || Number.isNaN(standard.value)) {
    ElMessage.warning('请输入有效的标准值。')
    return
  }
  points.value = [...points.value, {measured: average.value, standard: Number(standard.value)}]
  standard.value = null
  await updateFit()
  emitUpdate()
  ElMessage.success(`已添加第 ${points.value.length} 个标定点`)
}

function onRemovePoint(index) {
  points.value = points.value.filter((_, i) => i !== index)
  updateFit()
  emitUpdate()
}

async function onClearPoints() {
  if (!points.value.length) return
  try {
    await ElMessageBox.confirm('确定清空当前通道的全部标定点？', '清空标定点', {type: 'warning'})
  } catch (e) {
    return
  }
  points.value = []
  await updateFit()
  emitUpdate()
}

async function onDownload() {
  if (!props.running) {
    ElMessage.warning('请先启动该通道，再把标定数据写入下位机。')
    return
  }
  if (!props.channel) return
  if (!downloadRegisters.value) {
    ElMessage.warning('请至少添加 1 个标定点。')
    return
  }
  const addr = Number(startAddress.value) || 0
  if (addr + downloadRegisters.value > 65536) {
    ElMessage.error(`起始地址 ${addr} 加上 ${downloadRegisters.value} 个寄存器超出上限 65535，请调整。`)
    return
  }

  let summary = downloadSummary.value
  if (mode.value === 1) {
    if (!fit.value.ok) {
      ElMessage.warning('斜率截距至少需要 2 个实测值不同的标定点。')
      return
    }
    summary += `\n内容：斜率 k = ${fit.value.slopeText}，截距 b = ${fit.value.interceptText}`
  } else {
    summary += `\n内容：标定表 ${points.value.length} 组，每组先写实测均值再写标准值`
  }

  try {
    await ElMessageBox.confirm(`${summary}\n\n确定下载？`, '下载标定', {type: 'warning'})
  } catch (e) {
    return
  }

  downloading.value = true
  try {
    const n = await DownloadCalibration({
      channelId: props.channel.id,
      startAddress: addr,
      dataType: dataType.value,
      byteOrder: byteOrder.value,
      mode: mode.value,
      points: points.value.map(p => ({measured: p.measured, standard: p.standard}))
    })
    ElMessage.success(`已下载 ${n} 个寄存器到地址 ${addr}`)
    emitUpdate()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    downloading.value = false
  }
}

// 实时采样：只取选中点位且质量良好的值，同时间戳不重复计入
EventsOn('tag-values', payload => {
  if (!props.visible || !props.channel) return
  if (payload.channelId !== props.channel.id) return
  for (const v of payload.values) {
    if (v.tagId !== tagId.value) continue
    if (v.quality !== 'Good') continue
    if (v.numericValue === null || v.numericValue === undefined) continue
    if (lastTagId === v.tagId && lastStamp === v.timestampMs) continue
    lastTagId = v.tagId
    lastStamp = v.timestampMs
    samples.value = [...samples.value, {t: v.timestampMs, v: v.numericValue}]
    trimSamples()
  }
})
</script>

<style scoped>
.calib-top {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  padding-bottom: 10px;
  border-bottom: 1px solid #e6ebf2;
}

.field {
  display: flex;
  align-items: center;
  gap: 6px;
}

.field label {
  font-size: 12px;
  color: #6b7686;
  white-space: nowrap;
}

.calib-state {
  margin-left: auto;
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 10px;
}

.calib-state.ok {
  background: #e7f5ec;
  color: #1f7a3d;
}

.calib-state.bad {
  background: #fdecec;
  color: #b03030;
}

.calib-body {
  margin-top: 10px;
}

.block {
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  padding: 10px;
  background: #fbfcfe;
}

.block-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 13px;
  font-weight: 600;
  color: #24303f;
  margin-bottom: 8px;
}

.live {
  display: flex;
  gap: 10px;
  margin-bottom: 8px;
}

.live-item {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
}

.live-item label {
  font-size: 12px;
  color: #7a869a;
}

.live-item b {
  font-family: Consolas, monospace;
  font-size: 15px;
}

.live-now {
  color: #24303f;
}

.live-avg {
  color: #1f7a3d;
}

.add-row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.tip {
  margin: 6px 0 0;
  font-size: 12px;
  color: #8a94a6;
  line-height: 1.5;
}

.fit-box {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-top: 10px;
  padding: 8px 12px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
}

.fit-item {
  display: flex;
  flex-direction: column;
}

.fit-item label {
  font-size: 12px;
  color: #7a869a;
}

.fit-item b {
  font-family: Consolas, monospace;
  font-size: 14px;
  color: #24303f;
}

.fit-hint {
  margin-left: auto;
  font-size: 12px;
  color: #7a869a;
}

.calib-footer {
  display: flex;
  align-items: center;
  gap: 10px;
}

.footer-info {
  margin-right: auto;
  font-size: 12px;
  color: #7a869a;
  font-family: Consolas, monospace;
}
</style>
