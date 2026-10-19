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
        <div class="addr-field">
          <el-select
            v-model="form.addrFormat"
            size="small"
            class="addr-mode"
            @change="onAddrFormatChange"
          >
            <el-option v-for="f in ADDRESS_FORMATS" :key="f.value" :label="f.label" :value="f.value" />
          </el-select>
          <el-input
            v-model="form.addrText"
            size="small"
            class="addr-input"
            clearable
            :placeholder="addrPlaceholder"
          />
        </div>
        <div class="addr-hint" :class="{'addr-err': addrBad}">
          {{ addrHint }}
        </div>
      </el-form-item>
      <el-form-item label="数据类型">
        <el-select v-model="form.dataType">
          <el-option v-for="d in DATA_TYPES" :key="d.value" :label="d.label" :value="d.value" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="orderVisible" label="字节顺序">
        <el-select v-model="form.byteOrder" class="order-select">
          <el-option v-for="o in BYTE_ORDERS" :key="o.value" :label="o.label" :value="o.value" />
        </el-select>
        <div class="addr-hint">{{ orderHint }}</div>
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
import {
  ADDRESS_FORMATS,
  BYTE_ORDERS,
  DATA_TYPES,
  FUNCTION_CODES,
  addressFormatTip,
  addressTexts,
  byteOrderApplies,
  byteOrderTip
} from '../constants'
import {ParseAddressInput} from '../../wailsjs/go/main/App'

const props = defineProps({
  visible: {type: Boolean, default: false},
  tag: {type: Object, default: null},
  isNew: {type: Boolean, default: false},
  // 联动写入只能选本通道里可写的点（功能码 06/16），且不能选自己
  channel: {type: Object, default: null}
})
const emit = defineEmits(['update:visible', 'submit'])

const form = ref(null)

// 地址输入："形式 + 文本" 两块，真正的数值由后端 ParseAddressInput 判定。
// 判定放后端是因为 40001 到底是十进制还是 PLC 写法取决于所选形式，
// 规则一旦两处各写一份迟早会走偏，而后端有单测兜着。
const addrResult = ref(null)
const addrSeq = ref(0)
let addrTimer = 0

const writableTags = computed(() => {
  if (!props.channel || !props.channel.tags) return []
  const selfId = props.tag ? props.tag.id : ''
  return props.channel.tags.filter(t =>
    t.id !== selfId && (t.functionCode === 6 || t.functionCode === 16)
  )
})

// UInt16 只有一个寄存器，没有字序问题，把这一行藏掉免得看着像漏配
const orderVisible = computed(() => !!form.value && byteOrderApplies(form.value.dataType))

// 提示行把该字序在当前类型下的寄存器排布写出来，现场对着报文一眼就能对上
const orderHint = computed(() => {
  if (!form.value) return ''
  const hit = BYTE_ORDERS.find(o => o.value === form.value.byteOrder) || BYTE_ORDERS[0]
  const regs = form.value.dataType === 2 ? hit.regs64 : hit.regs32
  return `${hit.tip}；寄存器依次为 ${regs}`
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
  // 旧配置没有这个字段（undefined），回填成 0 = CDAB，行为与旧版一致
  t.byteOrder = Number.isFinite(Number(t.byteOrder)) ? Number(t.byteOrder) : 0
  // 地址按十进制回填，想用别的形式自行在下拉里切（切换时会自动换算）
  t.addrFormat = 'dec'
  t.addrText = String(t.address === undefined || t.address === null ? 0 : t.address)
  form.value = t
  addrResult.value = null
  scheduleParse()
})

// 切换形式时把当前值换算成该形式的写法，别让用户自己去翻 PLC 地址表
function onAddrFormatChange(f) {
  if (!form.value) return
  const base = addrResult.value && addrResult.value.ok
    ? addrResult.value.value
    : Number(form.value.address) || 0
  const texts = addressTexts(base)
  form.value.addrText = f === 'hex' ? texts.hex : f === 'plc' ? texts.plc : texts.dec
  scheduleParse()
}

const addrPlaceholder = computed(() => {
  const f = form.value ? form.value.addrFormat : 'dec'
  return f === 'hex' ? '0x0064' : f === 'plc' ? '40001' : '0 ~ 65535'
})

// 解析成功但落在不支持的区（线圈/离散输入/输入寄存器）也算不可用：
// 后端这时给的 plc 字段是按保持寄存器折算的，照着显示会把人带沟里，直接按警示出。
const addrUnusable = computed(() =>
  !!addrResult.value && addrResult.value.ok &&
  !!addrResult.value.area && addrResult.value.area !== 'holding'
)
const addrBad = computed(() =>
  (!!addrResult.value && !addrResult.value.ok) || addrUnusable.value
)

const addrHint = computed(() => {
  const r = addrResult.value
  if (!form.value) return ''
  if (!r) return addressFormatTip(form.value.addrFormat)
  if (!r.ok) return '✗ ' + r.error
  if (addrUnusable.value) {
    return '⚠ ' + (r.note ||
      ('这是' + areaLabel(r.area) + '区地址，本项目只支持保持寄存器（4xxxx）'))
  }
  const parts = [`协议地址 ${r.dec}`, `十六进制 ${r.hex}`, `PLC ${r.plc}`]
  if (r.note) parts.push(r.note)
  return '✓ ' + parts.join(' · ')
})

watch(() => (form.value ? form.value.addrText : ''), () => scheduleParse())

// 绑定调用是本机进程内的，很快，但边打字边请求没意义，稍等一拍再发
function scheduleParse() {
  if (addrTimer) window.clearTimeout(addrTimer)
  addrTimer = window.setTimeout(() => {
    addrTimer = 0
    runParse()
  }, 150)
}

async function runParse() {
  if (!form.value) return
  const text = String(form.value.addrText === undefined || form.value.addrText === null
    ? '' : form.value.addrText).trim()
  if (text === '') {
    addrResult.value = null
    return
  }
  const seq = ++addrSeq.value
  try {
    const res = await ParseAddressInput(form.value.addrFormat, text)
    if (seq === addrSeq.value) addrResult.value = res
  } catch (e) {
    if (seq === addrSeq.value) addrResult.value = {ok: false, error: String(e)}
  }
}

async function resolveAddress() {
  // 提交时不复用预览结果（可能被防抖漏掉最后一次），当场解析一遍为准
  const text = String(form.value.addrText === undefined || form.value.addrText === null
    ? '' : form.value.addrText).trim()
  if (text === '') {
    ElMessage.warning('请填写寄存器地址')
    return null
  }
  let res
  try {
    res = await ParseAddressInput(form.value.addrFormat, text)
  } catch (e) {
    ElMessage.warning(String(e))
    return null
  }
  if (!res.ok) {
    ElMessage.warning(res.error)
    return null
  }
  // 本项目功能码只有 03/06/16，读写不了其它区，存下来只会让现场排查半天
  if (res.area && res.area !== 'holding') {
    ElMessage.warning('这是' + areaLabel(res.area) + '区的地址，本项目只支持保持寄存器（4xxxx）')
    return null
  }
  addrResult.value = res
  return res
}

function areaLabel(area) {
  return {coil: '线圈', discrete: '离散输入', input: '输入寄存器', holding: '保持寄存器'}[area] || area
}

function parseNum(text) {
  const s = String(text === null || text === undefined ? '' : text).trim()
  if (s === '') return null
  const v = Number(s)
  return Number.isFinite(v) ? v : undefined // undefined 表示填了但不是数字
}

async function onOk() {
  if (!form.value) return
  if (!form.value.name || !form.value.name.trim()) {
    ElMessage.warning('请填写点名称')
    return
  }
  form.value.name = form.value.name.trim()
  if (typeof form.value.unit !== 'string') form.value.unit = ''

  const addr = await resolveAddress()
  if (!addr) return
  form.value.address = addr.value

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
  // 这两项只服务于输入框，不能落到点位配置里
  delete form.value.addrFormat
  delete form.value.addrText
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

.addr-field {
  display: flex;
  gap: 6px;
  width: 100%;
}

.addr-mode {
  width: 108px;
  flex-shrink: 0;
}

.addr-input {
  flex: 1;
  font-family: Consolas, monospace;
}

/* 解析结果/错误提示放在输入框下一行，不挤占表单宽度 */
.addr-hint {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: #909399;
  font-family: Consolas, monospace;
  word-break: break-all;
}

.addr-err {
  color: #d03030;
}

.order-select {
  width: 100%;
}
</style>
