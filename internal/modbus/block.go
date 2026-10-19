package modbus

import (
	"fmt"
	"sort"
	"strings"

	"modbus-hub-go/internal/models"
)

// 读块最多 125 个寄存器，写块最多 120 个
const (
	MaxReadRegisters  = 125
	MaxWriteRegisters = 120
)

// ReadBlock 一次 03 读保持寄存器请求的块
type ReadBlock struct {
	SlaveID byte
	Start   uint16
	Count   uint16
	Tags    []models.TagPoint
}

type blockBuilder struct {
	slaveID byte
	start   int
	end     int
	tags    []models.TagPoint
}

// SplitReadBlocks 按从机号 + 地址把点表拼成尽量少的读块
func SplitReadBlocks(tags []models.TagPoint) []ReadBlock {
	var blocks []ReadBlock
	groups := make(map[byte][]models.TagPoint)
	var order []byte
	for _, t := range tags {
		if _, ok := groups[t.SlaveID]; !ok {
			order = append(order, t.SlaveID)
		}
		groups[t.SlaveID] = append(groups[t.SlaveID], t)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	for _, slave := range order {
		group := groups[slave]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Address != group[j].Address {
				return group[i].Address < group[j].Address
			}
			return group[i].Name < group[j].Name
		})

		var cur *blockBuilder
		for _, tag := range group {
			tagEnd := int(tag.Address) + tag.RegisterCount()
			if cur == nil {
				cur = &blockBuilder{slaveID: slave, start: int(tag.Address), end: tagEnd, tags: []models.TagPoint{tag}}
				continue
			}
			newStart := min(cur.start, int(tag.Address))
			newEnd := max(cur.end, tagEnd)
			if newEnd-newStart > MaxReadRegisters {
				blocks = append(blocks, cur.build())
				cur = &blockBuilder{slaveID: slave, start: int(tag.Address), end: tagEnd, tags: []models.TagPoint{tag}}
			} else {
				cur.start = newStart
				cur.end = newEnd
				cur.tags = append(cur.tags, tag)
			}
		}
		if cur != nil {
			blocks = append(blocks, cur.build())
		}
	}
	return blocks
}

func (b *blockBuilder) build() ReadBlock {
	return ReadBlock{
		SlaveID: b.slaveID,
		Start:   uint16(b.start),
		Count:   uint16(b.end - b.start),
		Tags:    b.tags,
	}
}

// WriteItem 一次写入请求（点 + 文本值）
type WriteItem struct {
	Tag   models.TagPoint
	Value string
}

// EnsureWritableConsecutive 校验批量写入的点可写且地址首尾相接
func EnsureWritableConsecutive(tags []models.TagPoint) error {
	if len(tags) < 2 {
		return fmt.Errorf("请至少选择 2 个地址连续的可写点")
	}
	var readonly []string
	for _, t := range tags {
		if !t.IsWritable() {
			readonly = append(readonly, t.Name)
		}
	}
	if len(readonly) > 0 {
		return fmt.Errorf("批量写入只能包含功能码 06/16 的可写点：%s", strings.Join(readonly, "、"))
	}
	ordered := OrderByAddress(tags)
	for i := 0; i < len(ordered)-1; i++ {
		cur := ordered[i]
		next := ordered[i+1]
		end := int(cur.Address) + cur.RegisterCount()
		if int(next.Address) < end {
			return fmt.Errorf("地址重叠：%s（%d）与 %s（%d）", cur.Name, cur.Address, next.Name, next.Address)
		}
		if int(next.Address) > end {
			return fmt.Errorf("地址不连续：%s 占用到 %d，%s 从 %d 开始。请选择首尾相接的点",
				cur.Name, end-1, next.Name, next.Address)
		}
	}
	return nil
}

// OrderByAddress 按地址、名称排序
func OrderByAddress(tags []models.TagPoint) []models.TagPoint {
	out := make([]models.TagPoint, len(tags))
	copy(out, tags)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Address != out[j].Address {
			return out[i].Address < out[j].Address
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// EncodeBlock 把地址连续的点编成一段寄存器
func EncodeBlock(items []WriteItem) ([]uint16, error) {
	ordered := make([]WriteItem, len(items))
	copy(ordered, items)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Tag.Address < ordered[j].Tag.Address })

	tags := make([]models.TagPoint, 0, len(ordered))
	for _, it := range ordered {
		tags = append(tags, it.Tag)
	}
	if err := EnsureWritableConsecutive(tags); err != nil {
		return nil, err
	}

	var regs []uint16
	for _, it := range ordered {
		v, err := ParseInput(it.Tag, it.Value)
		if err != nil {
			return nil, fmt.Errorf("%s：%v", it.Tag.Name, err)
		}
		regs = append(regs, EncodeRegisters(v, it.Tag)...)
	}
	return regs, nil
}

// SplitByPduLimit 按 PDU 上限把写入项拆包
func SplitByPduLimit(items []WriteItem) [][]WriteItem {
	var chunks [][]WriteItem
	var cur []WriteItem
	regs := 0
	for _, it := range items {
		count := it.Tag.RegisterCount()
		if len(cur) > 0 && regs+count > MaxWriteRegisters {
			chunks = append(chunks, cur)
			cur = nil
			regs = 0
		}
		cur = append(cur, it)
		regs += count
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}
