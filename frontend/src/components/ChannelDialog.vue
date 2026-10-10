<template>
  <el-dialog
    :model-value="visible"
    :title="isNew ? '新增通道' : '通道参数'"
    width="560px"
    @update:model-value="v => emit('update:visible', v)"
  >
    <el-form v-if="form" label-width="110px" size="small">
      <el-form-item label="通道名称">
        <el-input v-model="form.name" placeholder="通道1" />
      </el-form-item>
      <el-form-item label="串口号">
        <div class="port-row">
          <el-select v-model="form.portName" filterable allow-create placeholder="COM1">
            <el-option v-for="p in ports" :key="p.name" :label="p.name" :value="p.name" />
          </el-select>
          <el-button @click="loadPorts">刷新</el-button>
        </div>
      </el-form-item>
      <el-form-item label="波特率">
        <el-select v-model="form.baudRate" filterable allow-create>
          <el-option v-for="b in BAUD_RATES" :key="b" :label="String(b)" :value="b" />
        </el-select>
      </el-form-item>
      <el-form-item label="数据位">
        <el-select v-model="form.dataBits">
          <el-option v-for="d in DATA_BITS_OPTIONS" :key="d.value" :label="d.label" :value="d.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="校验位">
        <el-select v-model="form.parity">
          <el-option v-for="p in PARITY_OPTIONS" :key="p.value" :label="p.label" :value="p.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="停止位">
        <el-select v-model="form.stopBits">
          <el-option v-for="s in STOP_BITS_OPTIONS" :key="s.value" :label="s.label" :value="s.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="从机号">
        <el-input-number v-model="form.slaveId" :min="1" :max="247" />
      </el-form-item>
      <el-form-item label="轮询周期(ms)">
        <el-input-number v-model="form.pollingIntervalMs" :min="100" :step="100" />
      </el-form-item>
      <el-form-item label="读超时(ms)">
        <el-input-number v-model="form.readTimeout" :min="100" :step="100" />
      </el-form-item>
      <el-form-item label="写超时(ms)">
        <el-input-number v-model="form.writeTimeout" :min="100" :step="100" />
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="form.isEnabled" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
      <el-button size="small" type="primary" @click="onOk">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import {ref, watch} from 'vue'
import {ElMessage} from 'element-plus'
import {ListPorts} from '../../wailsjs/go/main/App'
import {BAUD_RATES, DATA_BITS_OPTIONS, PARITY_OPTIONS, STOP_BITS_OPTIONS} from '../constants'

const props = defineProps({
  visible: {type: Boolean, default: false},
  channel: {type: Object, default: null},
  isNew: {type: Boolean, default: false}
})
const emit = defineEmits(['update:visible', 'submit'])

const form = ref(null)
const ports = ref([])

watch(() => props.visible, v => {
  if (v) {
    form.value = props.channel ? JSON.parse(JSON.stringify(props.channel)) : null
    if (!form.value.tags) form.value.tags = []
    loadPorts()
  }
})

async function loadPorts() {
  try {
    ports.value = await ListPorts()
  } catch (e) {
    ports.value = []
  }
}

function onOk() {
  if (!form.value) return
  if (!form.value.name || !form.value.name.trim()) {
    ElMessage.warning('请填写通道名称')
    return
  }
  if (!form.value.portName || !form.value.portName.trim()) {
    ElMessage.warning('请选择串口号')
    return
  }
  form.value.name = form.value.name.trim()
  form.value.portName = form.value.portName.trim()
  emit('submit', form.value)
  emit('update:visible', false)
}
</script>

<style scoped>
.port-row {
  display: flex;
  gap: 8px;
  width: 100%;
}
</style>
