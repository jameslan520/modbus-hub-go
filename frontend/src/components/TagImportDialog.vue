<template>
  <el-dialog
    :model-value="visible"
    title="批量导入点位"
    width="860px"
    top="5vh"
    :close-on-click-modal="false"
    @update:model-value="v => emit('update:visible', v)"
  >
    <div class="import-toolbar">
      <el-button size="small" type="primary" @click="onPick">选择 CSV 文件</el-button>
      <el-button size="small" @click="onTemplate">下载模板</el-button>
      <span class="import-path">{{ preview.path || '未选择文件' }}</span>
      <span class="addr-label">地址形式</span>
      <el-select v-model="addrFormat" size="small" class="addr-sel" @change="onFormatChange">
        <el-option v-for="f in ADDRESS_FORMATS" :key="f.value" :label="f.label" :value="f.value" />
      </el-select>
      <span class="import-hint">{{ addrTip }}</span>
    </div>

    <el-alert
      v-if="preview.errors && preview.errors.length"
      :title="`${preview.errors.length} 行无法导入（已跳过）`"
      type="warning"
      :closable="false"
      class="import-alert"
    >
      <div class="err-list">
        <div v-for="e in preview.errors.slice(0, 8)" :key="e.line" class="err-line">
          第 {{ e.line }} 行：{{ e.message }}
        </div>
        <div v-if="preview.errors.length > 8" class="err-line">
          …… 其余 {{ preview.errors.length - 8 }} 行错误略
        </div>
      </div>
    </el-alert>

    <div class="import-table">
      <el-table :data="preview.tags" size="small" height="260" border stripe>
        <el-table-column label="#" width="46">
          <template #default="{$index}">{{ $index + 1 }}</template>
        </el-table-column>
        <el-table-column prop="name" label="名称" min-width="110" />
        <el-table-column label="从机号" width="70">
          <template #default="{row}">{{ row.slaveId || '-' }}</template>
        </el-table-column>
        <el-table-column label="功能码" width="120">
          <template #default="{row}">{{ functionCodeLabel(row.functionCode) }}</template>
        </el-table-column>
        <el-table-column prop="address" label="地址" width="70" />
        <el-table-column label="类型" width="90">
          <template #default="{row}">{{ dataTypeLabel(row.dataType) }}</template>
        </el-table-column>
        <el-table-column prop="unit" label="单位" width="70" />
        <el-table-column label="倍率" width="70">
          <template #default="{row}">{{ row.scale }}</template>
        </el-table-column>
        <el-table-column label="偏移" width="70">
          <template #default="{row}">{{ row.offset }}</template>
        </el-table-column>
        <el-table-column label="小数位" width="70">
          <template #default="{row}">{{ row.decimalPlaces }}</template>
        </el-table-column>
        <el-table-column label="启用" width="60">
          <template #default="{row}">
            <el-tag size="small" :type="row.isEnabled ? 'success' : 'info'">
              {{ row.isEnabled ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="曲线" width="60">
          <template #default="{row}">
            <el-tag size="small" :type="row.trendEnabled ? 'success' : 'info'">
              {{ row.trendEnabled ? '是' : '否' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!preview.tags || !preview.tags.length"
        description="选择 CSV 后这里显示可导入的点位"
        :image-size="60"
      />
    </div>

    <div class="import-mode">
      <span class="mode-label">导入方式</span>
      <el-radio-group v-model="mode" size="small">
        <el-radio-button :value="0">追加</el-radio-button>
        <el-radio-button :value="1">合并（按名称+地址更新）</el-radio-button>
        <el-radio-button :value="2">覆盖（清空重建）</el-radio-button>
      </el-radio-group>
      <span class="mode-tip">{{ modeTip }}</span>
    </div>

    <template #footer>
      <span class="footer-count">
        可导入 {{ preview.tags ? preview.tags.length : 0 }} 个
        <template v-if="channel">，当前通道已有 {{ channel.tags.length }} 个</template>
      </span>
      <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
      <el-button
        size="small"
        type="primary"
        :disabled="!preview.tags || !preview.tags.length"
        @click="onConfirm"
      >
        导入
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import {computed, ref} from 'vue'
import {ElMessage} from 'element-plus'
import {dataTypeLabel, functionCodeLabel, ADDRESS_FORMATS, addressFormatTip} from '../constants'
import {
  ExportTagTemplate,
  ImportTags,
  ParseTagFile,
  PickTagFile
} from '../../wailsjs/go/main/App'

const props = defineProps({
  visible: {type: Boolean, default: false},
  channel: {type: Object, default: null}
})
const emit = defineEmits(['update:visible', 'imported'])

const preview = ref({path: '', tags: [], errors: []})
const mode = ref(1)
// 地址列的书写形式。整份文件统一用一种：混合的话 40001 无法判断是十进制还是 PLC 写法。
const addrFormat = ref('dec')
const addrTip = computed(() => addressFormatTip(addrFormat.value))

const MODES = [
  {value: 0, label: '追加', tip: '直接加到点表末尾，同名的会重复出现'},
  {value: 1, label: '合并', tip: '名称+地址相同的更新参数，其余新增；曲线和历史不会断'},
  {value: 2, label: '覆盖', tip: '清空当前点表后重建，旧点位的历史曲线会一并丢弃'}
]

const modeTip = computed(() => {
  const hit = MODES.find(m => m.value === mode.value)
  return hit ? hit.tip : ''
})

async function onPick() {
  try {
    const path = await PickTagFile()
    if (!path) return // 用户取消
    await parseFile(path)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

// 换个形式等于换一种读法，已经选好的文件要按新形式重新解析一遍
async function onFormatChange() {
  const path = preview.value.path
  if (!path) return
  try {
    await parseFile(path)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function parseFile(path) {
  const p = await ParseTagFile(path, addrFormat.value)
  preview.value = {
    path: p.path || path,
    tags: p.tags || [],
    errors: p.errors || []
  }
  if (!preview.value.tags.length) {
    ElMessage.warning('没有解析出可导入的点位')
  }
}

async function onTemplate() {
  try {
    const path = await ExportTagTemplate()
    if (path) ElMessage.success(`模板已导出：${path}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function onConfirm() {
  if (!props.channel) {
    ElMessage.warning('请先选择通道')
    return
  }
  try {
    const n = await ImportTags(props.channel.id, preview.value.tags, mode.value)
    ElMessage.success(`导入完成，当前共 ${n} 个点`)
    emit('imported')
    emit('update:visible', false)
  } catch (e) {
    ElMessage.error(String(e))
  }
}
</script>

<style scoped>
.import-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.import-path {
  font-family: Consolas, monospace;
  font-size: 12px;
  color: #24303f;
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.import-hint {
  margin-left: auto;
  font-size: 12px;
  color: #909399;
}

.addr-label {
  font-size: 12px;
  color: #606266;
}

.addr-sel {
  width: 104px;
}

.import-alert {
  margin-bottom: 8px;
}

.err-list {
  max-height: 96px;
  overflow: auto;
  font-size: 12px;
  line-height: 1.7;
}

.err-line {
  font-family: Consolas, monospace;
}

.import-table {
  height: 260px;
}

.import-mode {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  flex-wrap: wrap;
}

.mode-label {
  font-weight: 600;
}

.mode-tip {
  font-size: 12px;
  color: #909399;
}

.footer-count {
  float: left;
  line-height: 24px;
  font-size: 12px;
  color: #606266;
}
</style>
