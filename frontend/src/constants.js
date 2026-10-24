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

// 多寄存器数值的字节顺序。命名沿用现场习惯：数值最高字节记作 A，依次往下 B、C、D（32 位）/ … H（64 位）。
// Modbus 报文本身是大端（寄存器内高字节在前），"字序"说的是这些字节在报文里怎么排。
// 零值 CDAB 与旧版一致，旧配置读进来行为不变。
export const BYTE_ORDERS = [
  {value: 0, label: 'CDAB 低字在前', regs32: '[CD][AB]', regs64: 'GH EF CD AB',
   tip: '默认。32 位先发低字，64 位从最低字开始发'},
  {value: 1, label: 'ABCD 高字在前', regs32: '[AB][CD]', regs64: 'AB CD EF GH',
   tip: '大端直排，先发高字'},
  {value: 2, label: 'BADC 字内字节交换', regs32: '[BA][DC]', regs64: 'BA DC FE HG',
   tip: '每个寄存器内两字节对调，寄存器之间顺序不变'},
  {value: 3, label: 'DCBA 全反序', regs32: '[DC][BA]', regs64: 'HG FE DC BA',
   tip: '字节完全倒序（小端）'}
]

// 只有占多个寄存器的类型才有字序可言；UInt16 只有一个寄存器，界面上不显示
const BYTE_ORDER_TYPES = new Set([1, 2, 3]) // Float32 / Double64 / Int32

export function byteOrderApplies(dataType) {
  return BYTE_ORDER_TYPES.has(Number(dataType))
}

export function byteOrderLabel(v) {
  const hit = BYTE_ORDERS.find(d => d.value === v)
  return hit ? hit.label : 'CDAB'
}

// 下拉与悬浮提示用的一句话说明
export function byteOrderTip(v) {
  const hit = BYTE_ORDERS.find(d => d.value === v)
  return hit ? hit.tip : ''
}

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

// 寄存器地址的三种书面写法。
// 解析规则以 Go 端 modbus.ParseAddress 为准（那边有单测覆盖边界），
// 这里只负责"按哪种形式显示"，两边必须一致：hex 为 0x+4 位大写，
// plc 偏移 1~9999 用 5 位写法（40001），再大只能用 6 位（400001）。
export const ADDRESS_FORMATS = [
  {value: 'dec', label: '十进制', tip: '协议地址，0 起，范围 0~65535'},
  {value: 'hex', label: '十六进制', tip: '可写 0x0064、0064 或 64h'},
  {value: 'plc', label: 'PLC 地址', tip: '保持寄存器写作 40001（=协议地址 0），大地址用 400001'}
]

export function addressFormatTip(v) {
  const hit = ADDRESS_FORMATS.find(f => f.value === v)
  return hit ? hit.tip : ''
}

// 同一个地址的三种等价写法，用于点表悬浮提示与对话框预览
export function addressTexts(addr) {
  const n = Number.isFinite(Number(addr)) ? Number(addr) : 0
  const v = Math.min(65535, Math.max(0, Math.trunc(n)))
  return {
    dec: String(v),
    hex: '0x' + v.toString(16).toUpperCase().padStart(4, '0'),
    plc: v <= 9998 ? String(40001 + v) : String(400001 + v)
  }
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

// 写入窗口的输入口径。三个口径说的是同一个寄存器内容，只是站的角度不同；
// 换算一律由 Go 端 modbus.ParseWriteInput 完成，前端不重复实现，避免两边算得不一样。
export const INPUT_MODES = [
  {
    value: 'raw',
    label: '原始值',
    tip: '寄存器里真正存放的十进制数值，不乘倍率、不加偏移。可带单位后缀，会自动去掉。',
    placeholder: '例如 1234'
  },
  {
    value: 'hex',
    label: '十六进制',
    tip: '按 IEEE754 位模式填写，跟点表「十六进制」列显示的是同一套东西，高字节在前。' +
      '下发时再按本点的字节顺序拆成寄存器。可写 0x 前缀、h 后缀，空格与下划线会自动去掉。',
    placeholder: '例如 0x4048F5C3'
  },
  {
    value: 'eng',
    label: '工程值',
    tip: '现场读数，由程序按 原始值 = (工程值 − 偏移) ÷ 倍率 反算出报文内容。' +
      '整数类型反算出小数会就近取整，下发前请核一下预览。',
    placeholder: '例如 12.5'
  }
]

export function inputModeMeta(v) {
  return INPUT_MODES.find(m => m.value === v) || INPUT_MODES[0]
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
