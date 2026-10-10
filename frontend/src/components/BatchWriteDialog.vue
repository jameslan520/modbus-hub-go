<template>
  <el-dialog
    :model-value="visible"
    title="批量写入（功能码 16）"
    width="480px"
    @update:model-value="v => emit('update:visible', v)"
  >
    <el-alert
      type="info"
      :closable="false"
      show-icon
      title="所选点必须地址首尾相接且全部可写，按起始地址一次写入"
    />
    <el-table :data="rows" size="small" border class="batch-table">
      <el-table-column prop="name" label="名称" min-width="120" />
      <el-table-column prop="address" label="地址" width="80" />
      <el-table-column label="类型" width="100">
        <template #default="{row}">{{ dataTypeLabel(row.dataType) }}</template>
      </el-table-column>
      <el-table-column label="写入值" min-width="140">
        <template #default="{row}">
          <el-input v-model="inputMap[row.id]" size="small" placeholder="数值" />
        </template>
      </el-table-column>
    </el-table>
    <template #footer>
      <el-button size="small" @click="emit('update:visible', false)">取消</el-button>
      <el-button size="small" type="primary" @click="onOk">写入</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import {ref, watch} from 'vue'
import {ElMessage} from 'element-plus'
import {dataTypeLabel} from '../constants'

const props = defineProps({
  visible: {type: Boolean, default: false},
  tags: {type: Array, default: () => []}
})
const emit = defineEmits(['update:visible', 'submit'])

const rows = ref([])
const inputMap = ref({})

watch(() => props.visible, v => {
  if (v) {
    rows.value = [...props.tags].sort((a, b) => a.address - b.address)
    const map = {}
    rows.value.forEach(t => { map[t.id] = '' })
    inputMap.value = map
  }
})

function onOk() {
  const items = []
  for (const t of rows.value) {
    const v = (inputMap.value[t.id] || '').trim()
    if (v === '') {
      ElMessage.warning(`${t.name} 未填写写入值`)
      return
    }
    items.push({tagId: t.id, value: v})
  }
  if (!items.length) return
  emit('submit', items)
  emit('update:visible', false)
}
</script>

<style scoped>
.batch-table {
  margin-top: 8px;
}
</style>
