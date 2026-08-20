package main

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
)

// Database layout & layering
type Table struct {
	File string
	Head Header
}

type Header struct {
	Magic       [5]byte
	Version     uint8
	ColumnCount uint16
	RowCount    uint64
	Columns     []ColumnMeta
}

type ColumnMeta struct {
	Offset   uint64
	HasNulls bool
	Type     uint8
	Length   uint8
	Name     string
}

// NullBitmapOffset returns the start offset of the column's null bitmap.
func (c ColumnMeta) NullBitmapOffset(rowCount uint64, entrySize uint8) uint64 {
	bitmapSize := (rowCount + 7) / 8
	switch c.Type {
	case TypeString:
		return c.StringOffsetMapOffset(rowCount, entrySize) - bitmapSize
	case TypeDecimal:
		return c.DecimalPrecisionOffset() - bitmapSize
	default:
		return c.Offset - bitmapSize
	}
}

// StringEntrySizeOffset returns the offset of the column's entrySize marker byte.
func (c ColumnMeta) StringEntrySizeOffset() uint64 {
	return c.Offset - 1
}

// DecimalPrecisionOffset returns the offset of the column's precision marker byte.
func (c ColumnMeta) DecimalPrecisionOffset() uint64 {
	return c.Offset - 2
}

// DecimalScaleOffset returns the offset of the column's scale marker byte.
func (c ColumnMeta) DecimalScaleOffset() uint64 {
	return c.Offset - 1
}

// StringOffsetMapOffset returns the start offset of the column's string offset map.
func (c ColumnMeta) StringOffsetMapOffset(rowCount uint64, entrySize uint8) uint64 {
	return c.StringEntrySizeOffset() - uint64(entrySize)*(rowCount+1)
}

var MagicConst [5]byte = [5]byte{'j', 'a', 'c', 'k', 'y'}
var MaxInt32Size = 0x7FFFFFFF
var MaxUint32Size = 0xFFFFFFFF
var MaxInt8Size = 0x7F
var MaxInt16Size = 0x7FFF
var MaxUint8Size = 0xFF
var MaxUint16Size = 0xFFFF
var MaxDecimalPrecision uint8 = 18 // int64 cap; beyond this needs int128, which is deferred

// current format version; held at 1 until MVP
const CurrentVersion uint8 = 1

// Type Helpers
// Values descend from the top of the byte range (like a stack downward)
// so future types can claim the low end, including any that
// want to encode a small parameter directly in the type byte itself.
const (
	TypeInt8    uint8 = 0xFF
	TypeInt16   uint8 = 0xFE
	TypeInt32   uint8 = 0xFD
	TypeInt64   uint8 = 0xFC
	TypeUint8   uint8 = 0xFB
	TypeUint16  uint8 = 0xFA
	TypeUint32  uint8 = 0xF9
	TypeUint64  uint8 = 0xF8
	TypeFloat32 uint8 = 0xF7
	TypeFloat64 uint8 = 0xF6
	TypeBool    uint8 = 0xF5
	TypeDate    uint8 = 0xF4
	// TypeDecimal marks a fixed-point numeric column (precision + scale,
	// mirrors SQL NUMERIC). Byte width is derived from precision, not
	// stored directly - same idea as entrySize for TypeString.
	TypeDecimal uint8 = 0xF3
	// TypeString marks a dynamic string column (blob + offsets array).
	// A single dedicated tag, not a range trick like the old fixed-width
	// varchar(N)-via-type-byte scheme; downstream write/read code checks
	// against this value to know when to build the offsets array/entrySize.
	TypeString uint8 = 0xF2
)

// TypeToString returns a human-readable name for a type tag, e.g. "int32".
func TypeToString(t uint8) string {
	switch t {
	case TypeInt8:
		return "int8"
	case TypeInt16:
		return "int16"
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeUint8:
		return "uint8"
	case TypeUint16:
		return "uint16"
	case TypeUint32:
		return "uint32"
	case TypeUint64:
		return "uint64"
	case TypeFloat32:
		return "float32"
	case TypeFloat64:
		return "float64"
	case TypeBool:
		return "bool"
	case TypeDate:
		return "date"
	case TypeDecimal:
		return "decimal"
	case TypeString:
		return "string"
	default:
		return "unknown"
	}
}

var stringToType = map[string]uint8{
	"int8":    TypeInt8,
	"int16":   TypeInt16,
	"int32":   TypeInt32,
	"int64":   TypeInt64,
	"uint8":   TypeUint8,
	"uint16":  TypeUint16,
	"uint32":  TypeUint32,
	"uint64":  TypeUint64,
	"float32": TypeFloat32,
	"float64": TypeFloat64,
	"bool":    TypeBool,
	"date":    TypeDate,
	"decimal": TypeDecimal,
	"string":  TypeString,
}

// EncodeCell parses a cell's string value per colType and returns its
// fixed-width on-disk bytes. Never called for null cells; those are handled
// separately via the null bitmap.
func EncodeCell(colType uint8, cell string) ([]byte, error) {
	if colType == TypeDecimal {
		return nil, errors.New("TypeDecimal encoding not yet implemented")
	}

	buf := make([]byte, ByteSizeForType(colType))
	switch colType {
	case TypeInt8:
		v, err := strconv.ParseInt(cell, 10, 8)
		if err != nil {
			return nil, err
		}
		buf[0] = uint8(v)
	case TypeInt16:
		v, err := strconv.ParseInt(cell, 10, 16)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint16(buf, uint16(v))
	case TypeInt32:
		v, err := strconv.ParseInt(cell, 10, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, uint32(v))
	case TypeInt64:
		v, err := strconv.ParseInt(cell, 10, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, uint64(v))
	case TypeUint8:
		v, err := strconv.ParseUint(cell, 10, 8)
		if err != nil {
			return nil, err
		}
		buf[0] = uint8(v)
	case TypeUint16:
		v, err := strconv.ParseUint(cell, 10, 16)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint16(buf, uint16(v))
	case TypeUint32:
		v, err := strconv.ParseUint(cell, 10, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, uint32(v))
	case TypeUint64:
		v, err := strconv.ParseUint(cell, 10, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, v)
	case TypeFloat32:
		v, err := strconv.ParseFloat(cell, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, math.Float32bits(float32(v)))
	case TypeFloat64:
		v, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
	case TypeDate:
		// stored as unix timestamp (int64)
		v, err := strconv.ParseInt(cell, 10, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, uint64(v))
	case TypeBool:
		v, err := strconv.ParseBool(cell)
		if err != nil {
			return nil, err
		}
		if v {
			buf[0] = 1
		}
	default:
		return nil, errors.New("Passed type is invalid")
	}

	return buf, nil
}

// returns how many bytes each size takes up
var typeToByteSize = map[uint8]int{
	TypeInt8:    1,
	TypeInt16:   2,
	TypeInt32:   4,
	TypeInt64:   8,
	TypeUint8:   1,
	TypeUint16:  2,
	TypeUint32:  4,
	TypeUint64:  8,
	TypeFloat32: 4,
	TypeFloat64: 8,
	TypeBool:    1,
	TypeDate:    8, // stored as unix timestamp (int64)
}

// gets byte size for a type; not meaningful for TypeString, which is
// variable-width (blob + offsets array) and handled separately
func ByteSizeForType(t uint8) int {
	return typeToByteSize[t]
}

// EntrySizeForBlobLen returns the minimum byte width (1, 2, 4, or 8) needed
// to hold an offset up to blobLen: how wide each entry in a TypeString
// column's offset map needs to be.
func EntrySizeForBlobLen(blobLen uint64) uint8 {
	switch {
	case blobLen <= uint64(MaxUint8Size):
		return 1
	case blobLen <= uint64(MaxUint16Size):
		return 2
	case blobLen <= uint64(MaxUint32Size):
		return 4
	default:
		return 8
	}
}
