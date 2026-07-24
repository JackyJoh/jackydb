package main

import (
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
	Offset uint64
	Type   uint8
	Length uint8
	Name   string
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

var stringToType = map[string]uint8{
	"int32":   TypeInt32,
	"int64":   TypeInt64,
	"uint32":  TypeUint32,
	"uint64":  TypeUint64,
	"float64": TypeFloat64,
	"bool":    TypeBool,
	"date":    TypeDate,
	"numeric": TypeNumeric,
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
