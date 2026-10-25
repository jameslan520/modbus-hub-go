<template>
  <div class="panel channel-panel">
    <div class="panel-title">
      <span>通道列表</span>
      <el-button size="small" type="primary" @click="emit('add')">新增</el-button>
    </div>
    <div class="watch-tip">点通道=只看它；勾选=多个通道同屏显示</div>
    <div class="panel-body">
      <div
        v-for="ch in channels"
        :key="ch.id"
        class="channel-item"
        :class="{active: ch.id === currentId}"
        @click="emit('select', ch.id)"
      >
        <div class="channel-row">
          <span class="channel-check" @click.stop>
            <el-checkbox
              :model-value="watched(ch.id)"
              size="small"
              @change="v => emit('watch', ch.id, v)"
            />
          </span>
          <span class="channel-name">{{ ch.name }}</span>
          <el-tag size="small" :type="stateMeta(ch.id).type">{{ stateMeta(ch.id).label }}</el-tag>
        </div>
        <div class="channel-meta mono">
          <el-tag size="small" effect="plain" round>{{ connTypeLabel(ch.connType) }}</el-tag>
          {{ channelEndpointText(ch) }}
          <template v-if="Number(ch.connType) !== 1">
            · {{ parityLabel(ch.parity) }} · {{ stopBitsLabel(ch.stopBits) }}
          </template>
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
import {STATE_META, channelEndpointText, connTypeLabel, parityLabel, stopBitsLabel} from '../constants'

const props = defineProps({
  channels: {type: Array, default: () => []},
  runtime: {type: Object, default: () => ({})},
  currentId: {type: String, default: ''},
  // 勾选后并入点表监视（可以同时监视多个通道）
  watchedIds: {type: Array, default: () => []}
})
const emit = defineEmits(['select', 'add', 'edit', 'remove', 'start', 'stop', 'watch'])

function watched(id) {
  return props.watchedIds.includes(id)
}

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

.watch-tip {
  padding: 4px 10px;
  font-size: 12px;
  color: #909399;
  background: #f7f9fc;
  border-bottom: 1px solid #eef2f7;
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
  gap: 4px;
}

/* 勾选框要吃掉点击不再冒泡到整行（否则勾一次就变成"只看这个"了） */
.channel-check {
  display: inline-flex;
  flex-shrink: 0;
}

.channel-name {
  font-weight: 600;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.channel-meta {
  font-size: 12px;
  color: #6b7280;
  margin-top: 2px;
  display: flex;
  align-items: center;
  gap: 4px;
}

.channel-actions {
  margin-top: 6px;
  display: flex;
  gap: 4px;
}
</style>
