<template>
  <div class="panel tag-panel">
    <div class="panel-title">
      <div class="tag-title-left">
        <span>点表{{ panelLabel }}</span>
        <span v-if="multi && primary" class="base-hint">
          新增 / 导入 / 导出作用于「{{ primary.name }}」
        </span>
        <el-radio-group v-model="base" size="small">
          <el-radio-button value="dec">工程值</el-radio-button>
          <el-radio-button value="hex">十六进制</el-radio-button>
          <el-radio-button value="bin">二进制</el-radio-button>
          <el-radio-button value="reg">寄存器</el-radio-button>
        </el-radio-group>
      </div>
      <div>
        <el-button size="small" type="primary" :disabled="!primary" @click="emit('add')">新增点</el-button>
        <el-button size="small" :disabled="!selection.length" @click="onBatch">批量写入</el-button>
        <el-button size="small" :disabled="!trendSelection.length" @click="onCompare">对比曲线</el-button>
        <el-button size="small" :disabled="!primary" @click="emit('import')">导入 CSV</el-button>
        <el-button size="small" :disabled="!primary || !rows.length" @click="emit('export-def')">
          导出定义
        </el-button>
      </div>
    </div>
    <div class="panel-body">
      <el-table
        v-if="rows.length || primary"
        :data="rows"
        row-key="id"
        size="small"
        height="100%"
        border
        :row-class-name="rowClass"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column v-if="multi" prop="channelName" label="通道" min-width="110" />
        <el-table-column prop="name" label="名称" min-width="110" />
        <el-table-column label="地址" width="76">
          <template #default="{row}">
            <el-tooltip :content="addressTip(row)" placement="top" :show-after="200">
              <span class="mono">{{ row.address }}</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{row}">
            <el-tooltip :content="typeTip(row)" placement="top" :show-after="200" :disabled="!typeTip(row)">
              <span>{{ dataTypeLabel(row.dataType) }}<span v-if="typeOrderLabel(row)" class="order-tag">{{ typeOrderLabel(row) }}</span></span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="功能码" width="140">
          <template #default="{row}">{{ functionCodeLabel(row.functionCode) }}</template>
        </el-table-column>
        <el-table-column :min-width="baseMeta.minWidth" class-name="col-value">
          <template #header>
            <el-tooltip :content="baseMeta.tip" placement="top" :show-after="200">
              <span class="col-header">
                {{ baseMeta.label }}
                <el-icon><InfoFilled /></el-icon>
              </span>
            </el-tooltip>
          </template>
          <template #default="{row}">
            <span
              :class="valueClass(row)"
              :title="cellTitle(row)"
              @click="copyValue(row)"
            >{{ displayValue(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="报警" width="96">
          <template #default="{row}">
            <el-tag
              v-if="alarmState(row)"
              size="small"
              effect="dark"
              :type="alarmKindType(alarmState(row))"
            >
              {{ alarmKindLabel(alarmState(row)) }}
            </el-tag>
            <el-tag v-else-if="hasAlarm(row)" size="small" type="success">正常</el-tag>
            <span v-else class="alarm-off">未配置</span>
          </template>
        </el-table-column>
        <el-table-column label="质量" width="80">
          <template #default="{row}">
            <el-tag size="small" :type="quality(row) === 'Good' ? 'success' : 'danger'">
              {{ quality(row) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="140">
          <template #default="{row}">{{ timestamp(row) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{row}">
            <el-button size="small" :disabled="!row.trendEnabled" @click="onTrend(row)">曲线</el-button>
            <el-button size="small" :disabled="!row.trendEnabled" @click="emit('history', row)">历史</el-button>
            <el-button size="small" :disabled="!isWritable(row)" @click="onWrite(row)">写入</el-button>
            <el-button size="small" @click="emit('edit', row)">编辑</el-button>
            <el-button size="small" type="danger" @click="emit('remove', row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="请在左侧选择要监视的通道" :image-size="80" />
    </div>
  </div>
</template>

<script setup>
import {computed, ref} from 'vue'
import {ElMessage} from 'element-plus'
import {InfoFilled} from '@element-plus/icons-vue'
import {
  addressTexts,
  alarmKindLabel,
  alarmKindType,
  byteOrderApplies,
  byteOrderLabel,
  byteOrderTip,
  dataTypeLabel,
  functionCodeLabel
} from '../constants'

// 实时值的四种显示口径：
// dec 工程值（按倍率/小数位/单位），hex/bin 是该值的机器位模式（浮点数取 IEEE754），
// reg 是报文里的原始寄存器（按线序，未做字序重排）——排查字序问题时看它最直观。
// minWidth 按最长的内容算：hex 要放下 Double64 的 0x+16 位，reg 要放下 4 个寄存器，
// bin 要放下 32 位（64 位允许按空格折成两行）。
const BASE_META = {
  dec: {
    label: '实时值',
    tip: '按点配置的倍率、小数位与单位换算后的工程值',
    minWidth: 120
  },
  hex: {
    label: '十六进制',
    tip: '该值的机器位模式：UInt16 16 位，Float32/Int32 32 位（浮点数按 IEEE754），Double64 64 位',
    minWidth: 160
  },
  bin: {
    label: '二进制',
    tip: '同上，以二进制按 4 位一组展示；Double64 为 64 位，会折成两行',
    minWidth: 430
  },
  reg: {
    label: '原始寄存器',
    tip: '报文中该点位占用的原始寄存器，按线序排列（未做字序重排）',
    minWidth: 200
  }
}

const props = defineProps({
  // 已按通道合并好的点位行，每行带 channelId / channelName 标记归属
  rows: {type: Array, default: () => []},
  // 主通道：新增点、导入、导出定义都作用在它身上
  primary: {type: Object, default: null},
  // 同时监视多个通道时显示「通道」列
  multi: {type: Boolean, default: false},
  values: {type: Object, default: () => ({})}
})
const emit = defineEmits([
  'add', 'edit', 'remove', 'write', 'batch-write', 'trend',
  'history', 'compare', 'import', 'export-def'
])

const selection = ref([])
const base = ref('dec')
const baseMeta = computed(() => BASE_META[base.value] || BASE_META.dec)

const panelLabel = computed(() => {
  if (props.multi) return ` — 监视 ${props.rows.length} 点 / 多通道`
  return props.primary ? ` — ${props.primary.name}` : ''
})

function tagValue(row) {
  return props.values[row.id]
}

// 地址列默认显示十进制协议地址，三种写法挂在悬浮提示里：
// 现场对着 PLC 图纸找点时，手上多半是 40001 这种写法。
function addressTip(row) {
  const t = addressTexts(row.address)
  return `十进制 ${t.dec}\n十六进制 ${t.hex}\nPLC 地址 ${t.plc}`
}

function displayValue(row) {
  const v = tagValue(row)
  if (!v || v.quality !== 'Good') {
    return v && v.displayValue ? v.displayValue : '--'
  }
  switch (base.value) {
    case 'hex':
      return v.hexValue || '--'
    case 'bin':
      return v.binValue || '--'
    case 'reg':
      return registersText(v.registers)
    default:
      return v.displayValue || '--'
  }
}

function registersText(regs) {
  if (!regs || !regs.length) return '--'
  return regs
    .map(r => '0x' + Number(r & 0xffff).toString(16).toUpperCase().padStart(4, '0'))
    .join(' ')
}

function cellTitle(row) {
  return `${baseMeta.value.tip}\n点击复制：${displayValue(row)}`
}

async function copyValue(row) {
  const text = displayValue(row)
  if (!text || text === '--') return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success({message: `已复制 ${row.name} 的${baseMeta.value.label}`, duration: 1200})
  } catch (e) {
    ElMessage.warning('复制失败，请手动选中文本复制')
  }
}

function quality(row) {
  const v = tagValue(row)
  return v && v.quality ? v.quality : 'Bad'
}

// 报警方向由后端随采集值下发，前端不重复判阈值
function alarmState(row) {
  const v = tagValue(row)
  return v && v.alarmState ? v.alarmState : ''
}

// 配了报警（启用了且有任一限值）才显示"正常"，否则提示未配置
// 只有多寄存器类型才带字序标记；UInt16 没有字序，不显示免得看着像漏配
function typeOrderLabel(row) {
  return byteOrderApplies(row.dataType) ? byteOrderLabel(row.byteOrder) : ''
}

// 悬浮提示写清这个字序下寄存器怎么排，现场对着报文一眼能核对
function typeTip(row) {
  if (!byteOrderApplies(row.dataType)) return ''
  return `字节顺序 ${byteOrderLabel(row.byteOrder)}：${byteOrderTip(row.byteOrder)}`
}

function hasAlarm(row) {
  return !!row.alarmEnabled &&
    (row.alarmHigh !== null && row.alarmHigh !== undefined ||
      row.alarmLow !== null && row.alarmLow !== undefined)
}

function rowClass({row}) {
  return alarmState(row) ? 'row-alarm' : ''
}

const trendSelection = computed(() => selection.value.filter(t => t.trendEnabled))

function onCompare() {
  if (!trendSelection.value.length) {
    ElMessage.warning('请勾选至少 1 个启用了曲线记录的点位')
    return
  }
  if (trendSelection.value.length > 8) {
    ElMessage.warning('一次最多对比 8 个点位')
    return
  }
  emit('compare', trendSelection.value)
}

function timestamp(row) {
  const v = tagValue(row)
  return v && v.timestamp ? v.timestamp : ''
}

function valueClass(row) {
  return quality(row) === 'Good' ? 'value-good' : 'value-bad'
}

function isWritable(row) {
  return row.functionCode === 6 || row.functionCode === 16
}

function onSelectionChange(rows) {
  selection.value = rows
}

function onWrite(row) {
  if (!isWritable(row)) {
    ElMessage.warning('只读点（功能码 03）不可写入')
    return
  }
  emit('write', row)
}

function onTrend(row) {
  if (!row.trendEnabled) {
    ElMessage.warning('该点位未启用曲线记录，请在点参数中勾选后重试')
    return
  }
  emit('trend', row)
}

function onBatch() {
  const writable = selection.value.filter(isWritable)
  if (writable.length < 2) {
    ElMessage.warning('请至少勾选 2 个地址连续的可写点（功能码 06/16）')
    return
  }
  // 功能码 16 是一帧写多个寄存器，天然不能跨串口，跨通道只能拆成多次请求。
  // 批量写入对话框按地址排序并要求首尾相接，混着两个通道会排出一条假序列，所以这里直接拦。
  const channels = new Set(writable.map(t => t.channelId || ''))
  if (channels.size > 1) {
    ElMessage.warning('批量写入只能在同一个通道内，请分开勾选')
    return
  }
  emit('batch-write', writable)
}
</script>

<style scoped>
.tag-panel {
  flex: 1;
  min-height: 0;
}

.tag-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.base-hint {
  font-weight: 400;
  font-size: 12px;
  color: #909399;
  margin-left: 4px;
}

.col-header {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.mono {
  font-family: Consolas, monospace;
}

/* 类型列里的字序标记：小一号、浅色，不抢主类型的视觉重点 */
.order-tag {
  margin-left: 4px;
  font-size: 11px;
  color: #909399;
  font-family: Consolas, monospace;
}

/* 二进制 64 位（79 字符）放不进一行时，只允许在 4 位组的空格处折行，
   不能按字符硬断（否则 0x 前缀和位组会被切断）。td 带 class-name="col-value"，
   但 .cell 是 el-table 内部元素，必须穿透 scoped。 */
:deep(.col-value .cell) {
  white-space: normal;
  word-break: keep-all;
  overflow-wrap: normal;
  text-overflow: clip;
}

.value-good {
  font-weight: 600;
  color: #1f7a3d;
  font-family: Consolas, monospace;
  cursor: pointer;
}

.value-bad {
  color: #b03030;
  font-family: Consolas, monospace;
  cursor: pointer;
}

.alarm-off {
  color: #c0c4cc;
  font-size: 12px;
}

/* 报警行整行染色，扫一眼就能定位 */
:deep(.row-alarm) {
  background: #fdf0f0;
}

:deep(.row-alarm:hover > td) {
  background: #fbe3e3 !important;
}
</style>
