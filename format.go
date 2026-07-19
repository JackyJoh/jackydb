package main

type Table struct {
	File string
	Head Header
}

type Header struct {
	Magic       [5]byte
	ColumnCount uint16
	Rows        uint64
	Columns     []ColumnMeta
}

type ColumnMeta struct {
	Offset uint64
	Type   uint8
	Length uint8
	Name   string
}
