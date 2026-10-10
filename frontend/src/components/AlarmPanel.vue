<template>
  <div class="alarm-panel">
    <div class="alarm-toolbar">
      <el-tag :type="active.length ? 'danger' : 'success'" size="small" effect="dark">
        当前报警 {{ active.length }}
      </el-tag>
      <el-button size="small" :disabled="!unackedCount" @click="emit('ack-all')">
        全部确认（{{ unackedCount }}）
      </el-button>
      <el-button size="small" :disabled="!history.length" @click="emit('clear')">清空历史</el-button>
      <el-button size="small" :disabled="!history.length" @click="emit('export')">导出 CSV</el-button>
      <el-button size="small" @click="emit('refresh')">刷新</el-button>
      <span class="alarm-hint">报警在采集值超限时触发，回落到限值 ∓ 回差以内才恢复</span>
    </div>

    <el-table :data="active" size="small" height="100%" border :row-class-name="() => 'row-active'">
      <el-table-column prop="timestamp" label="触发时间" width="150" />
      <el-table-column prop="channelName" label="通道" width="110" />
      <el-table-column prop="tagName" label="点位" min-width="110" />
      <el-table-column label="方向" width="100">
        <template #default="{row}">
          <el-tag size="small" effect="dark" :type="alarmKindType(row.kind)">
            {{ alarmKindLabel(row.kind) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="当前值" width="110">
        <template #default="{row}">{{ valueText(row) }}</template>
      </el-table-column>
      <el-table-column label="限值" width="110">
        <template #default="{row}">{{ valueText(row, true) }}</template>
      </el-table-column>
      <el-table-column prop="action" label="联动结果" min-width="150" show-overflow-tooltip />
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{row}">
          <el-button size="small" :disabled="row.acked" @click="emit('ack', row.id)">
            {{ row.acked ? '已确认' : '确认' }}
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="当前没有报警" :image-size="60" />
      </template>
    </el-table>
  </div>
</template>

<script setup>
import {computed} from 'vue'
import {alarmKindLabel, alarmKindType} from '../constants'

const props = defineProps({
  active: {type: Array, default: () => []},
  history: {type: Array, default: () => []}
})
const emit = defineEmits(['ack', 'ack-all', 'clear', 'export', 'refresh'])

const unackedCount = computed(() => props.active.filter(a => !a.acked).length)

function valueText(row, isLimit) {
  const v = isLimit ? row.limit : row.value
  if (v === null || v === undefined) return '--'
  const text = Number(v).toPrecision(6)
  return row.unit ? `${Number(text)} ${row.unit}` : String(Number(text))
}
</script>

<style scoped>
.alarm-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.alarm-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-bottom: 1px solid #ebeef5;
  flex-wrap: wrap;
}

.alarm-hint {
  margin-left: auto;
  font-size: 12px;
  color: #909399;
}

:deep(.row-active) {
  background: #fdf0f0;
}
</style>
