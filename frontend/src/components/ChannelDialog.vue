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
      <el-form-item label="连接方式">
        <el-radio-group v-model="form.connType">
          <el-radio-button v-for="c in CONN_TYPES" :key="c.value" :value="c.value">
            {{ c.label }}
          </el-radio-button>
        </el-radio-group>
        <div class="conn-tip">{{ connTip }}</div>
      </el-form-item>

      <!-- 串口参数 -->
      <template v-if="isSerial">
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
      </template>

      <!-- 网口参数 -->
      <template v-else>
        <el-form-item label="目标 IP">
          <el-input v-model="form.host" placeholder="192.168.1.10" />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="form.port" :min="1" :max="65535" />
          <span class="port-hint">Modbus TCP 默认 {{ DEFAULT_TCP_PORT }}</span>
        </el-form-item>
        <el-form-item label="单元标识">
          <el-input-number v-model="form.slaveId" :min="1" :max="247" />
          <span class="port-hint">串口透传网关背后挂的真实从机号，不是网关自己的地址</span>
        </el-form-item>
      </template>

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
import {computed, ref, watch} from 'vue'
import {ElMessage} from 'element-plus'
import {ListPorts} from '../../wailsjs/go/main/App'
import {
  BAUD_RATES, CONN_TYPES, DATA_BITS_OPTIONS, DEFAULT_TCP_PORT,
  PARITY_OPTIONS, STOP_BITS_OPTIONS, connTypeMeta
} from '../constants'

const props = defineProps({
  visible: {type: Boolean, default: false},
  channel: {type: Object, default: null},
  isNew: {type: Boolean, default: false}
})
const emit = defineEmits(['update:visible', 'submit'])

const form = ref(null)
const ports = ref([])

const isSerial = computed(() => Number(form.value?.connType) !== 1)
const connTip = computed(() => connTypeMeta(form.value?.connType).tip)

watch(() => props.visible, v => {
  if (v) {
    form.value = props.channel ? JSON.parse(JSON.stringify(props.channel)) : null
    if (!form.value.tags) form.value.tags = []
    // 旧配置没有connType（= undefined），当串口处理；网口缺端口时补默认值
    if (form.value.connType === undefined || form.value.connType === null) form.value.connType = 0
    if (!form.value.port) form.value.port = DEFAULT_TCP_PORT
    if (isSerial.value) loadPorts()
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
  form.value.name = form.value.name.trim()

  if (isSerial.value) {
    if (!form.value.portName || !form.value.portName.trim()) {
      ElMessage.warning('请选择串口号')
      return
    }
    form.value.portName = form.value.portName.trim()
  } else {
    const host = (form.value.host || '').trim()
    if (!host) {
      ElMessage.warning('请填写目标 IP 或主机名')
      return
    }
    form.value.host = host
    if (!form.value.port || form.value.port < 1 || form.value.port > 65535) {
      ElMessage.warning(`端口应在 1~65535 之间，Modbus TCP 一般用 ${DEFAULT_TCP_PORT}`)
      return
    }
  }
  emit('submit', form.value)
  emit('update:visible', false)
}
</script>

<style scoped>
.conn-tip {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
  line-height: 1.4;
}

.port-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.port-hint {
  margin-left: 8px;
  font-size: 12px;
  color: #909399;
}
</style>
