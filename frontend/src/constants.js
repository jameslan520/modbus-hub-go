// 与 Go 端 models 保持一致的枚举与选项

export const DATA_TYPES = [
  {value: 0, label: 'UInt16'},
  {value: 1, label: 'Float32'},
  {value: 2, label: 'Double64'},
  {value: 3, label: 'Int32'}
]

export const FUNCTION_CODES = [
  {value: 3, label: '03 读保持寄存器'},
  {value: 6, label: '06 写单个寄存器'},
  {value: 16, label: '16 写多个寄存器'}
]

export const PARITY_OPTIONS = [
  {value: 0, label: '无校验 (None)'},
  {value: 1, label: '奇校验 (Odd)'},
  {value: 2, label: '偶校验 (Even)'}
]

export const STOP_BITS_OPTIONS = [
  {value: 1, label: '1 位'},
  {value: 2, label: '2 位'}
]

export const DATA_BITS_OPTIONS = [
  {value: 7, label: '7 位'},
  {value: 8, label: '8 位'}
]

export const BAUD_RATES = [9600, 19200, 38400, 57600, 115200]

export const STATE_META = {
  Disconnected: {label: '离线', type: 'info'},
  Connecting: {label: '连接中', type: 'warning'},
  Connected: {label: '已连接', type: 'success'},
  Error: {label: '故障', type: 'danger'}
}

export function dataTypeLabel(v) {
  const hit = DATA_TYPES.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

export function functionCodeLabel(v) {
  const hit = FUNCTION_CODES.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

export function parityLabel(v) {
  const hit = PARITY_OPTIONS.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

export function stopBitsLabel(v) {
  const hit = STOP_BITS_OPTIONS.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

// 曲线时间窗口（秒），0 表示全部历史
export const TREND_WINDOWS = [
  {value: 60, label: '1 分钟'},
  {value: 300, label: '5 分钟'},
  {value: 900, label: '15 分钟'},
  {value: 0, label: '全部'}
]

export function trendWindowLabel(v) {
  const hit = TREND_WINDOWS.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

// 标定：取平均的采样点数
export const CALIB_SAMPLE_COUNTS = [5, 8, 10, 15, 20]

// 标定：标定表 / 斜率截距
export const CALIB_WRITE_MODES = [
  {value: 0, label: '标定表（实测、标准交替）'},
  {value: 1, label: '斜率与截距（k、b）'}
]

export function calibModeLabel(v) {
  const hit = CALIB_WRITE_MODES.find(d => d.value === v)
  return hit ? hit.label : String(v)
}

// 报警方向：与 Go 端 models.AlarmKind 一致
export const ALARM_KINDS = [
  {value: 'High', label: '超上限', type: 'danger'},
  {value: 'Low', label: '低于下限', type: 'warning'}
]

export function alarmKindLabel(v) {
  const hit = ALARM_KINDS.find(d => d.value === v)
  return hit ? hit.label : ''
}

export function alarmKindType(v) {
  const hit = ALARM_KINDS.find(d => d.value === v)
  return hit ? hit.type : 'info'
}

// 历史回放的时间范围（小时）
export const HISTORY_RANGES = [
  {value: 1, label: '最近 1 小时'},
  {value: 6, label: '最近 6 小时'},
  {value: 24, label: '最近 24 小时'},
  {value: 72, label: '最近 3 天'},
  {value: 168, label: '最近 7 天'},
  {value: 0, label: '全部'}
]

// 曲线配色：多点位对比时按序取用
export const SERIES_COLORS = [
  '#2f6fd0', '#d97a2b', '#1f9d55', '#b03030', '#7a52c9',
  '#0f8f9e', '#c2579b', '#8a6d3b'
]

// 毫秒时间戳 -> HH:MM:SS
export function clockText(ms) {
  if (!ms) return '--'
  return new Date(ms).toLocaleTimeString('zh-CN', {hour12: false})
}

// 毫秒时间戳 -> 完整时间（含毫秒）
export function stampText(ms) {
  if (!ms) return '--'
  const d = new Date(ms)
  const p = n => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
    `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${String(d.getMilliseconds()).padStart(3, '0')}`
}
