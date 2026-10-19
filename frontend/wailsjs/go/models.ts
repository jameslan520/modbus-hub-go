export namespace alarm {
	
	export class Event {
	    id: string;
	    channelId: string;
	    channelName: string;
	    tagId: string;
	    tagName: string;
	    kind: string;
	    active: boolean;
	    value: number;
	    limit: number;
	    unit: string;
	    timestamp: string;
	    timestampMs: number;
	    message: string;
	    acked: boolean;
	    action?: string;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channelId = source["channelId"];
	        this.channelName = source["channelName"];
	        this.tagId = source["tagId"];
	        this.tagName = source["tagName"];
	        this.kind = source["kind"];
	        this.active = source["active"];
	        this.value = source["value"];
	        this.limit = source["limit"];
	        this.unit = source["unit"];
	        this.timestamp = source["timestamp"];
	        this.timestampMs = source["timestampMs"];
	        this.message = source["message"];
	        this.acked = source["acked"];
	        this.action = source["action"];
	    }
	}

}

export namespace hub {
	
	export class ChannelRuntime {
	    id: string;
	    state: string;
	    txCount: number;
	    rxCount: number;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChannelRuntime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.state = source["state"];
	        this.txCount = source["txCount"];
	        this.rxCount = source["rxCount"];
	        this.running = source["running"];
	    }
	}
	export class TrendSeries {
	    tagId: string;
	    name: string;
	    unit: string;
	    high?: number;
	    low?: number;
	    points: trend.Point[];
	
	    static createFrom(source: any = {}) {
	        return new TrendSeries(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.name = source["name"];
	        this.unit = source["unit"];
	        this.high = source["high"];
	        this.low = source["low"];
	        this.points = this.convertValues(source["points"], trend.Point);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WriteTagRequest {
	    tagId: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new WriteTagRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.value = source["value"];
	    }
	}

}

export namespace main {
	
	export class CalibrationRequest {
	    channelId: string;
	    startAddress: number;
	    dataType: number;
	    byteOrder: number;
	    mode: number;
	    points: models.CalibrationPoint[];
	
	    static createFrom(source: any = {}) {
	        return new CalibrationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channelId = source["channelId"];
	        this.startAddress = source["startAddress"];
	        this.dataType = source["dataType"];
	        this.byteOrder = source["byteOrder"];
	        this.mode = source["mode"];
	        this.points = this.convertValues(source["points"], models.CalibrationPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FitResult {
	    ok: boolean;
	    slope: number;
	    intercept: number;
	    slopeText: string;
	    interceptText: string;
	    hint: string;
	
	    static createFrom(source: any = {}) {
	        return new FitResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.slope = source["slope"];
	        this.intercept = source["intercept"];
	        this.slopeText = source["slopeText"];
	        this.interceptText = source["interceptText"];
	        this.hint = source["hint"];
	    }
	}
	export class PruneResult {
	    files: number;
	    freed: number;
	
	    static createFrom(source: any = {}) {
	        return new PruneResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = source["files"];
	        this.freed = source["freed"];
	    }
	}

}

export namespace modbus {
	
	export class AddressResult {
	    ok: boolean;
	    value: number;
	    area: string;
	    error: string;
	    note: string;
	    dec: string;
	    hex: string;
	    plc: string;
	
	    static createFrom(source: any = {}) {
	        return new AddressResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.value = source["value"];
	        this.area = source["area"];
	        this.error = source["error"];
	        this.note = source["note"];
	        this.dec = source["dec"];
	        this.hex = source["hex"];
	        this.plc = source["plc"];
	    }
	}

}

export namespace models {
	
	export class CalibrationPoint {
	    measured: number;
	    standard: number;
	
	    static createFrom(source: any = {}) {
	        return new CalibrationPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.measured = source["measured"];
	        this.standard = source["standard"];
	    }
	}
	export class TagPoint {
	    id: string;
	    name: string;
	    slaveId: number;
	    functionCode: number;
	    address: number;
	    dataType: number;
	    byteOrder: number;
	    unit: string;
	    scale: number;
	    offset: number;
	    decimalPlaces: number;
	    trendEnabled: boolean;
	    isEnabled: boolean;
	    alarmEnabled: boolean;
	    alarmHigh?: number;
	    alarmLow?: number;
	    alarmDeadband: number;
	    alarmDelaySec: number;
	    alarmActionTag?: string;
	    alarmActionText?: string;
	
	    static createFrom(source: any = {}) {
	        return new TagPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slaveId = source["slaveId"];
	        this.functionCode = source["functionCode"];
	        this.address = source["address"];
	        this.dataType = source["dataType"];
	        this.byteOrder = source["byteOrder"];
	        this.unit = source["unit"];
	        this.scale = source["scale"];
	        this.offset = source["offset"];
	        this.decimalPlaces = source["decimalPlaces"];
	        this.trendEnabled = source["trendEnabled"];
	        this.isEnabled = source["isEnabled"];
	        this.alarmEnabled = source["alarmEnabled"];
	        this.alarmHigh = source["alarmHigh"];
	        this.alarmLow = source["alarmLow"];
	        this.alarmDeadband = source["alarmDeadband"];
	        this.alarmDelaySec = source["alarmDelaySec"];
	        this.alarmActionTag = source["alarmActionTag"];
	        this.alarmActionText = source["alarmActionText"];
	    }
	}
	export class ChannelConfig {
	    id: string;
	    name: string;
	    portName: string;
	    baudRate: number;
	    dataBits: number;
	    parity: number;
	    stopBits: number;
	    readTimeout: number;
	    writeTimeout: number;
	    pollingIntervalMs: number;
	    slaveId: number;
	    isEnabled: boolean;
	    tags: TagPoint[];
	    calibrationTagId?: string;
	    calibrationSampleCount: number;
	    calibrationStartAddress: number;
	    calibrationDataType: number;
	    calibrationByteOrder: number;
	    calibrationWriteMode: number;
	    calibrationPoints: CalibrationPoint[];
	
	    static createFrom(source: any = {}) {
	        return new ChannelConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.portName = source["portName"];
	        this.baudRate = source["baudRate"];
	        this.dataBits = source["dataBits"];
	        this.parity = source["parity"];
	        this.stopBits = source["stopBits"];
	        this.readTimeout = source["readTimeout"];
	        this.writeTimeout = source["writeTimeout"];
	        this.pollingIntervalMs = source["pollingIntervalMs"];
	        this.slaveId = source["slaveId"];
	        this.isEnabled = source["isEnabled"];
	        this.tags = this.convertValues(source["tags"], TagPoint);
	        this.calibrationTagId = source["calibrationTagId"];
	        this.calibrationSampleCount = source["calibrationSampleCount"];
	        this.calibrationStartAddress = source["calibrationStartAddress"];
	        this.calibrationDataType = source["calibrationDataType"];
	        this.calibrationByteOrder = source["calibrationByteOrder"];
	        this.calibrationWriteMode = source["calibrationWriteMode"];
	        this.calibrationPoints = this.convertValues(source["calibrationPoints"], CalibrationPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LogEntry {
	    timestamp: string;
	    level: string;
	    channelName: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.level = source["level"];
	        this.channelName = source["channelName"];
	        this.message = source["message"];
	    }
	}
	
	export class TagValue {
	    tagId: string;
	    rawValue: any;
	    numericValue?: number;
	    displayValue: string;
	    quality: string;
	    timestamp: string;
	    timestampMs: number;
	    registers?: number[];
	    hexValue?: string;
	    binValue?: string;
	    alarmState?: string;
	    errorMessage?: string;
	
	    static createFrom(source: any = {}) {
	        return new TagValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.rawValue = source["rawValue"];
	        this.numericValue = source["numericValue"];
	        this.displayValue = source["displayValue"];
	        this.quality = source["quality"];
	        this.timestamp = source["timestamp"];
	        this.timestampMs = source["timestampMs"];
	        this.registers = source["registers"];
	        this.hexValue = source["hexValue"];
	        this.binValue = source["binValue"];
	        this.alarmState = source["alarmState"];
	        this.errorMessage = source["errorMessage"];
	    }
	}

}

export namespace serialx {
	
	export class PortInfo {
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new PortInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	    }
	}

}

export namespace store {
	
	export class Range {
	    from: number;
	    to: number;
	    count: number;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Range(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = source["from"];
	        this.to = source["to"];
	        this.count = source["count"];
	        this.bytes = source["bytes"];
	    }
	}
	export class Settings {
	    enabled: boolean;
	    retentionDays: number;
	    maxSizeMB: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.retentionDays = source["retentionDays"];
	        this.maxSizeMB = source["maxSizeMB"];
	    }
	}
	export class Stats {
	    enabled: boolean;
	    tagCount: number;
	    fileCount: number;
	    totalBytes: number;
	    oldestDay: string;
	    newestDay: string;
	    retentionDays: number;
	    maxSizeMB: number;
	    root: string;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.tagCount = source["tagCount"];
	        this.fileCount = source["fileCount"];
	        this.totalBytes = source["totalBytes"];
	        this.oldestDay = source["oldestDay"];
	        this.newestDay = source["newestDay"];
	        this.retentionDays = source["retentionDays"];
	        this.maxSizeMB = source["maxSizeMB"];
	        this.root = source["root"];
	    }
	}

}

export namespace tagio {
	
	export class RowError {
	    line: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RowError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.message = source["message"];
	    }
	}
	export class Preview {
	    path: string;
	    header: string[];
	    total: number;
	    tags: models.TagPoint[];
	    errors: RowError[];
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.header = source["header"];
	        this.total = source["total"];
	        this.tags = this.convertValues(source["tags"], models.TagPoint);
	        this.errors = this.convertValues(source["errors"], RowError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace trend {
	
	export class Point {
	    t: number;
	    v: number;
	
	    static createFrom(source: any = {}) {
	        return new Point(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.v = source["v"];
	    }
	}
	export class Stats {
	    min: number;
	    max: number;
	    avg: number;
	    last: number;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.min = source["min"];
	        this.max = source["max"];
	        this.avg = source["avg"];
	        this.last = source["last"];
	        this.count = source["count"];
	    }
	}

}

