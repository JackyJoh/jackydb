package main

type Header struct {
	Magic       [5]byte
	ColumnCount uint16
	Columns     []ColumnMeta
	Rows        uint64
}

type ColumnMeta struct {
	Offset uint64
	Type   uint8
	Length uint8
	Name   string
}
