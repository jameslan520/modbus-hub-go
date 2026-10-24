<template>
  <el-dialog
    :model-value="visible"
    :title="title"
    width="560px"
    :close-on-click-modal="false"
    @update:model-value="v => emit('update:visible', v)"
  >
    <div class="w-head">
      <div class="w-meta">
        地址 {{ tag.address }} · {{ dataTypeLabel(tag.dataType) }}
        <template v-if="orderLabel"> · 字节顺序 {{ orderLabel }}</template>
        <template v-if="tag.unit"> · 单位 {{ tag.unit }}</template>
      </div>
      <div v-if="scaleText" class="w-meta dim">点表换算：{{ scaleText }}</div>
    </div>

    <el-radio-group v-model="mode" size="small" class="w-modes" @change="onModeChange">
      <el-radio-button v-for="m in INPUT_MODES" :key="m.value" :value="m.value">
        {{ m.label }}
      </el-radio-button>
    </el-radio-group>
    <div class="w-tip">{{ modeMeta.tip }}</div>

    <el-input
      ref="inputEl"
      v-model="text"
      size="large"
      class="w-input"
      :placeholder="modeMeta.placeholder"
      @keyup.enter="onOk"
    />

    <div class="w-preview">
      <template v-if="preview.ok">
        <div class="pv-row">
          <label>原始值</label><b>{{ preview.rawText }}</b>
          <span class="pv-note">写进寄存器的十进制内容</span>
        </div>
        <div class="pv-row">
          <label>工程值</label><b>{{ preview.engText }}</b>
          <span class="pv-note">回读后点表上显示的值</span>
        </div>
        <div class="pv-row">
          <label>位模式</label><b>{{ preview.hexText }}</b>
          <span class="pv-note">与点表「十六进制」列同一口径</span>
        </div>
        <div class="pv-row">
          <label>寄存器</label>
          <b class="regs">{{ preview.registers.join(' ') }}</b>
          <span class="pv-note">按{{ orderLabel || '当前' }}字节顺序发出</span>
        </div>
        <div v-if="preview.formula" class="pv-formula">{{ preview.formula }}</div>
        <div v-if="preview.roundNote" class="pv-round">{{ preview.roundNote }}</div>
      </template>
      <template v-else-if="preview.error">
        <div class="pv-error">{{ preview.error }}</div>
      </template>
      <template v-else>
        <div class="pv-idle">输入数值后这里显示将要下发的寄存器内容</div>
      </template>
    </div>

    <template #footer>
      <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
      <el-button size="small" type="primary" :disabled="!preview.ok" @click="onOk">写入</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import {computed, nextTick, ref, watch} from 'vue'
import {INPUT_MODES, byteOrderApplies, byteOrderLabel, dataTypeLabel, inputModeMeta} from '../constants'
import {PreviewWrite} from '../../wailsjs/go/main/App'

const props = defineProps({
  visible: {type: Boolean, default: false},
  tag: {type: Object, default: null},
  // 点位所属通道：预览要按通道查找点位定义
  channelId: {type: String, default: ''}
})
const emit = defineEmits(['update:visible', 'submit'])

const inputEl = ref(null)
const mode = ref('raw')
const text = ref('')
const preview = ref({ok: false, error: '', rawText: '', engText: '', hexText: '', registers: [], formula: '', roundNote: ''})
const empty = () => ({
  ok: false, error: '', rawText: '', engText: '', hexText: '', registers: [], formula: '', roundNote: ''
})

let seq = 0

const modeMeta = computed(() => inputModeMeta(mode.value))

// 单寄存器类型没有字序，不显示免得看着像漏配
const orderLabel = computed(() => {
  if (!props.tag || !byteOrderApplies(props.tag.dataType)) return ''
  return byteOrderLabel(props.tag.byteOrder)
})

const title = computed(() => `写入 · ${props.tag ? props.tag.name : ''}`)

// 把点表里的倍率/偏移如实列出来，工程值口径下现场能对着核
const scaleText = computed(() => {
  const t = props.tag
  if (!t) return ''
  const scale = t.scale === null || t.scale === undefined ? 1 : Number(t.scale)
  const offset = t.offset === null || t.offset === undefined ? 0 : Number(t.offset)
  if (scale === 1 && offset === 0) return ''
  return `工程值 = 原始值 × ${scale}${offset ? (offset > 0 ? ' + ' : ' − ') + Math.abs(offset) : ''}`
})

watch(() => props.visible, v => {
  if (!v) return
  mode.value = 'raw'
  text.value = ''
  preview.value = empty()
  nextTick(() => {
    if (inputEl.value) inputEl.value.focus()
  })
})

// 切换口径时同一串文本含义完全不同，直接清空比让它变个数更安全
function onModeChange() {
  text.value = ''
  preview.value = empty()
  nextTick(() => {
    if (inputEl.value) inputEl.value.focus()
  })
}

watch(text, v => {
  runPreview(v)
})

// 预览走后端，前端不重复实现换算；用序号丢弃过期回包，
// 免得快速输入时先发的请求后到，把预览刷成旧值
function runPreview(v) {
  const t = props.tag
  if (!t || !String(v).trim()) {
    preview.value = empty()
    return
  }
  const mine = ++seq
  PreviewWrite(props.channelId, t.id, String(v).trim(), mode.value)
    .then(p => {
      if (mine !== seq) return
      preview.value = {
        ok: !!p.ok,
        error: p.error || '',
        rawText: p.rawText || '',
        engText: p.engText || '',
        hexText: p.hexText || '',
        registers: p.registers || [],
        formula: p.formula || '',
        roundNote: p.roundNote || ''
      }
    })
    .catch(e => {
      if (mine !== seq) return
      preview.value = { ...empty(), error: String(e) }
    })
}

function onOk() {
  if (!preview.value.ok) return
  emit('submit', {tagId: props.tag.id, value: String(text.value).trim(), mode: mode.value})
  emit('update:visible', false)
}
</script>

<style scoped>
.w-head {
  margin-bottom: 10px;
}

.w-meta {
  font-size: 12px;
  color: #606266;
  font-family: Consolas, monospace;
}

.w-meta.dim {
  margin-top: 2px;
  color: #9aa5b5;
}

.w-modes {
  margin-bottom: 6px;
}

.w-tip {
  font-size: 12px;
  line-height: 1.7;
  color: #8a94a6;
  margin-bottom: 8px;
  min-height: 34px;
}

.w-preview {
  margin-top: 12px;
  padding: 10px 12px;
  background: #f4f7fb;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
  min-height: 116px;
}

.pv-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 2px 0;
}

.pv-row label {
  width: 52px;
  flex: none;
  font-size: 12px;
  color: #7a869a;
}

.pv-row b {
  font-family: Consolas, monospace;
  font-size: 13px;
  color: #24303f;
}

.pv-row b.regs {
  color: #1f6f3f;
  letter-spacing: 0.5px;
}

.pv-note {
  font-size: 11px;
  color: #9aa5b5;
}

.pv-formula {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed #d3dbe6;
  font-family: Consolas, monospace;
  font-size: 12px;
  color: #2f6fd0;
}

.pv-round {
  margin-top: 4px;
  font-size: 12px;
  color: #b06f1e;
}

.pv-error {
  font-size: 13px;
  color: #c45656;
  padding: 8px 0;
}

.pv-idle {
  font-size: 12px;
  color: #9aa5b5;
  padding: 8px 0;
}
</style>
