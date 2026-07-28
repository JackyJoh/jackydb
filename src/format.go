package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// NullBitmapOffset returns where this column's null bitmap starts, derived
// from Offset and the table's row count (the bitmap always sits immediately
// before the column's data). Only meaningful when HasNulls is true.
func (c ColumnMeta) NullBitmapOffset(rowCount uint64) uint64 {
	return c.Offset - (rowCount+7)/8
}

var MagicConst [5]byte = [5]byte{'j', 'a', 'c', 'k', 'y'}
var MaxInt32Size = 0x7FFFFFFF
var MaxUint32Size = 0xFFFFFFFF

// current format version as of today
const CurrentVersion uint8 = 1

// Type Helpers
const (
	TypeInt32   uint8 = 0x01
	TypeInt64   uint8 = 0x02
	TypeUint32  uint8 = 0x03
	TypeUint64  uint8 = 0x04
	TypeFloat64 uint8 = 0x05
	TypeBool    uint8 = 0x06
	// 0x07 retired (was TypeString; varchar supersedes it)
	TypeDate    uint8 = 0x08
	TypeNumeric uint8 = 0x09
)

// anything bigger than TypeNumeric = varchar(N), N is just the byte value itself
const (
	TypeVarcharMin     uint8 = TypeNumeric + 1 // 0x0A
	TypeVarcharDefault uint8 = 32              // varchar(32)
)

// is it a varchar?
func IsVarchar(t uint8) bool {
	return t > TypeNumeric
}

// the type byte IS the max length
func VarcharMaxLen(t uint8) uint8 {
	return t
}

// makes a varchar(N) type byte, defaults to 32 if maxLen too small
func NewVarcharType(maxLen uint8) uint8 {
	if maxLen <= TypeNumeric {
		return TypeVarcharDefault
	}
	return maxLen
}

// TypeToString returns a human-readable name for a type tag, e.g. "int32"
// or "varchar(50)".
func TypeToString(t uint8) string {
	if IsVarchar(t) {
		return fmt.Sprintf("varchar(%d)", VarcharMaxLen(t))
	}
	switch t {
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeUint32:
		return "uint32"
	case TypeUint64:
		return "uint64"
	case TypeFloat64:
		return "float64"
	case TypeBool:
		return "bool"
	case TypeDate:
		return "date"
	case TypeNumeric:
		return "numeric"
	default:
		return "unknown"
	}
}

var stringToType = map[string]uint8{
	"int32":   TypeInt32,
	"int64":   TypeInt64,
	"uint32":  TypeUint32,
	"uint64":  TypeUint64,
	"float64": TypeFloat64,
	"bool":    TypeBool,
	"date":    TypeDate,
	"numeric": TypeNumeric,
	"varchar": TypeVarcharDefault,
}

// EncodeCell parses a cell's string value per colType and returns its
// fixed-width on-disk bytes. Never called for null cells; those are handled
// separately via the null bitmap.
func EncodeCell(colType uint8, cell string) ([]byte, error) {
	if IsVarchar(colType) {
		buf := make([]byte, VarcharMaxLen(colType))
		copy(buf, cell) // length already validated <= VarcharMaxLen by TestType
		return buf, nil
	}

	buf := make([]byte, ByteSizeForType(colType))
	switch colType {
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
	case TypeFloat64, TypeNumeric:
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

// ParseVarcharType parses "varchar(N)" or "varcharN" into a sized varchar
// type tag via NewVarcharType. Returns ok=false if s isn't a varchar form.
func ParseVarcharType(s string) (uint8, bool) {
	if !strings.HasPrefix(s, "varchar") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.Trim(strings.TrimPrefix(s, "varchar"), "()"))
	if err != nil || n <= 0 || n > 255 {
		return 0, false
	}
	return NewVarcharType(uint8(n)), true
}

// returns how many bytes each size takes up
var typeToByteSize = map[uint8]int{
	TypeInt32:   4,
	TypeInt64:   8,
	TypeUint32:  4,
	TypeUint64:  8,
	TypeFloat64: 8,
	TypeBool:    1,
	TypeDate:    8, // stored as unix timestamp (int64)
	TypeNumeric: 8, // stored as float64
}

// gets byte size for a type, handles varchar too
func ByteSizeForType(t uint8) int {
	if IsVarchar(t) {
		return int(VarcharMaxLen(t))
	}
	return typeToByteSize[t]
}
