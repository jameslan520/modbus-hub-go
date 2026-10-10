<template>
  <div class="panel channel-panel">
    <div class="panel-title">
      <span>通道列表</span>
      <el-button size="small" type="primary" @click="emit('add')">新增</el-button>
    </div>
    <div class="panel-body">
      <div
        v-for="ch in channels"
        :key="ch.id"
        class="channel-item"
        :class="{active: ch.id === currentId}"
        @click="emit('select', ch.id)"
      >
        <div class="channel-row">
          <span class="channel-name">{{ ch.name }}</span>
          <el-tag size="small" :type="stateMeta(ch.id).type">{{ stateMeta(ch.id).label }}</el-tag>
        </div>
        <div class="channel-meta mono">
          {{ ch.portName }} · {{ ch.baudRate }} · {{ parityLabel(ch.parity) }} · {{ stopBitsLabel(ch.stopBits) }}
        </div>
        <div class="channel-meta mono">
          从机 {{ ch.slaveId }} · {{ ch.pollingIntervalMs }}ms · 发 {{ rt(ch.id).txCount }} / 收 {{ rt(ch.id).rxCount }}
        </div>
        <div class="channel-actions">
          <el-button size="small" :disabled="rt(ch.id).running" @click.stop="emit('start', ch.id)">启动</el-button>
          <el-button size="small" :disabled="!rt(ch.id).running" @click.stop="emit('stop', ch.id)">停止</el-button>
          <el-button size="small" @click.stop="emit('edit', ch)">编辑</el-button>
          <el-button size="small" type="danger" @click.stop="emit('remove', ch)">删除</el-button>
        </div>
      </div>
      <el-empty v-if="!channels.length" description="暂无通道" :image-size="60" />
    </div>
  </div>
</template>

<script setup>
import {STATE_META, parityLabel, stopBitsLabel} from '../constants'

const props = defineProps({
  channels: {type: Array, default: () => []},
  runtime: {type: Object, default: () => ({})},
  currentId: {type: String, default: ''}
})
const emit = defineEmits(['select', 'add', 'edit', 'remove', 'start', 'stop'])

function rt(id) {
  return props.runtime[id] || {state: 'Disconnected', txCount: 0, rxCount: 0, running: false}
}

function stateMeta(id) {
  return STATE_META[rt(id).state] || STATE_META.Disconnected
}
</script>

<style scoped>
.channel-panel {
  width: 300px;
  flex-shrink: 0;
}

.channel-item {
  padding: 8px 10px;
  border-bottom: 1px solid #f0f2f5;
  cursor: pointer;
}

.channel-item:hover {
  background: #f5f9ff;
}

.channel-item.active {
  background: #e8f2ff;
  border-left: 3px solid #1f3a5f;
}

.channel-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.channel-name {
  font-weight: 600;
}

.channel-meta {
  font-size: 12px;
  color: #6b7280;
  margin-top: 2px;
}

.channel-actions {
  margin-top: 6px;
  display: flex;
  gap: 4px;
}
</style>
