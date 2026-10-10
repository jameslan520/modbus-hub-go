<template>
  <el-dialog
    :model-value="visible"
    :title="isNew ? '新增数据点' : '数据点参数'"
    width="560px"
    top="4vh"
    @update:model-value="v => emit('update:visible', v)"
  >
    <el-form v-if="form" label-width="110px" size="small">
      <el-form-item label="点名称">
        <el-input v-model="form.name" />
      </el-form-item>
      <el-form-item label="从机号">
        <el-input-number v-model="form.slaveId" :min="1" :max="247" />
      </el-form-item>
      <el-form-item label="功能码">
        <el-select v-model="form.functionCode">
          <el-option v-for="f in FUNCTION_CODES" :key="f.value" :label="f.label" :value="f.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="寄存器地址">
        <el-input-number v-model="form.address" :min="0" :max="65535" />
      </el-form-item>
      <el-form-item label="数据类型">
        <el-select v-model="form.dataType">
          <el-option v-for="d in DATA_TYPES" :key="d.value" :label="d.label" :value="d.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="单位">
        <el-input v-model="form.unit" placeholder="如 °C、MPa" />
      </el-form-item>
      <el-form-item label="倍率">
        <el-input-number v-model="form.scale" :precision="4" :step="0.1" />
      </el-form-item>
      <el-form-item label="偏移">
        <el-input-number v-model="form.offset" :precision="4" :step="0.1" />
      </el-form-item>
      <el-form-item label="小数位">
        <el-input-number v-model="form.decimalPlaces" :min="0" :max="12" />
      </el-form-item>
      <el-form-item label="启用采集">
        <el-switch v-model="form.isEnabled" />
      </el-form-item>
      <el-form-item label="记录趋势">
        <el-switch v-model="form.trendEnabled" />
      </el-form-item>

      <el-divider content-position="left">报警</el-divider>
      <el-form-item label="启用报警">
        <el-switch v-model="form.alarmEnabled" :disabled="!hasLimit" />
        <span v-if="!hasLimit" class="field-hint">至少填一个限值才能启用</span>
      </el-form-item>
      <el-form-item label="上限">
        <el-input v-model="form.alarmHighText" clearable placeholder="留空表示不判上限" />
      </el-form-item>
      <el-form-item label="下限">
        <el-input v-model="form.alarmLowText" clearable placeholder="留空表示不判下限" />
      </el-form-item>
      <el-form-item label="回差">
        <el-input-number v-model="form.alarmDeadband" :min="0" :precision="3" :step="0.5" />
        <span class="field-hint">回落到限值 ∓ 回差以内才算恢复，防止临界抖动</span>
      </el-form-item>
      <el-form-item label="确认延时">
        <el-input-number v-model="form.alarmDelaySec" :min="0" :max="3600" />
        <span class="field-hint">秒；持续超限这么久才报警，0 为立即</span>
      </el-form-item>
      <el-form-item label="联动写入">
        <el-select
          v-model="form.alarmActionTag"
          clearable
          placeholder="不联动"
          class="action-select"
        >
          <el-option v-for="t in writableTags" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
        <el-input
          v-model="form.alarmActionText"
          placeholder="报警时写入的值"
          class="action-value"
          :disabled="!form.alarmActionTag"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
      <el-button size="small" type="primary" @click="onOk">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import {computed, ref, watch} from 'vue'
import {ElMessage} from 'element-plus'
import {DATA_TYPES, FUNCTION_CODES} from '../constants'

const props = defineProps({
  visible: {type: Boolean, default: false},
  tag: {type: Object, default: null},
  isNew: {type: Boolean, default: false},
  // 联动写入只能选本通道里可写的点（功能码 06/16），且不能选自己
  channel: {type: Object, default: null}
})
const emit = defineEmits(['update:visible', 'submit'])

const form = ref(null)

const writableTags = computed(() => {
  if (!props.channel || !props.channel.tags) return []
  const selfId = props.tag ? props.tag.id : ''
  return props.channel.tags.filter(t =>
    t.id !== selfId && (t.functionCode === 6 || t.functionCode === 16)
  )
})

// 上限/下限用文本输入：留空代表不判该方向（Go 端是指针字段，null 即不启用）
const hasLimit = computed(() => {
  if (!form.value) return false
  return parseNum(form.value.alarmHighText) !== null || parseNum(form.value.alarmLowText) !== null
})

watch(() => props.visible, v => {
  if (!v || !props.tag) return
  const t = JSON.parse(JSON.stringify(props.tag))
  t.alarmHighText = t.alarmHigh === null || t.alarmHigh === undefined ? '' : String(t.alarmHigh)
  t.alarmLowText = t.alarmLow === null || t.alarmLow === undefined ? '' : String(t.alarmLow)
  t.alarmDeadband = t.alarmDeadband || 0
  t.alarmDelaySec = t.alarmDelaySec || 0
  t.alarmActionTag = t.alarmActionTag || ''
  t.alarmActionText = t.alarmActionText || ''
  t.alarmEnabled = !!t.alarmEnabled
  form.value = t
})

function parseNum(text) {
  const s = String(text === null || text === undefined ? '' : text).trim()
  if (s === '') return null
  const v = Number(s)
  return Number.isFinite(v) ? v : undefined // undefined 表示填了但不是数字
}

function onOk() {
  if (!form.value) return
  if (!form.value.name || !form.value.name.trim()) {
    ElMessage.warning('请填写点名称')
    return
  }
  form.value.name = form.value.name.trim()
  if (typeof form.value.unit !== 'string') form.value.unit = ''

  const high = parseNum(form.value.alarmHighText)
  const low = parseNum(form.value.alarmLowText)
  if (high === undefined || low === undefined) {
    ElMessage.warning('报警限值是数字，留空表示不判该方向')
    return
  }
  form.value.alarmHigh = high
  form.value.alarmLow = low
  if (high !== null && low !== null && high <= low) {
    ElMessage.warning('上限必须大于下限')
    return
  }
  if (!form.value.alarmEnabled || (high === null && low === null)) {
    form.value.alarmEnabled = false
    form.value.alarmActionTag = ''
    form.value.alarmActionText = ''
  }
  if (form.value.alarmActionTag && !form.value.alarmActionText.trim()) {
    ElMessage.warning('选了联动点位就要填写入值')
    return
  }
  delete form.value.alarmHighText
  delete form.value.alarmLowText
  emit('submit', form.value)
  emit('update:visible', false)
}
</script>

<style scoped>
.field-hint {
  margin-left: 8px;
  font-size: 12px;
  color: #909399;
}

.action-select {
  width: 150px;
}

.action-value {
  width: 170px;
  margin-left: 8px;
}
</style>
