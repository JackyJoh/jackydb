package main

import (
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type TypeTracker struct {
	isNumeric  bool
	isDecimal  bool
	isOnlyPos  bool
	maxVal     uint64 // use isOnlyPos for signs
	isBoolVals bool
	maxCellLen uint8 // largest cell byte length seen; used to size varchar(N) fallback
}

// Flags start "on" (true) and get flipped "off" (false) as violating cells are seen
func NewTypeTracker() TypeTracker {
	return TypeTracker{
		isNumeric:  true,
		isDecimal:  false,
		isOnlyPos:  true,
		maxVal:     0,
		isBoolVals: true,
		maxCellLen: 0,
	}
}

// UpdateFlags inspects a single cell for a column and flips tracker flags off
// flags only move one-way (true -> false); flags cannot be re-enabled.
// Returns an error if the cell can't be represented at all (exceeds the
// 255-byte varchar ceiling of the format).
func (t *TypeTracker) UpdateFlags(cell string) error {
	// numeric check (covers int/uint/float)
	if t.isNumeric {
		f, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			t.isNumeric = false
		} else {
			if f < 0 {
				t.isOnlyPos = false
			}
			// maxVal must come from an exact integer parse, not  float64
			if strings.ContainsAny(cell, ".eE") {
				t.isDecimal = true
			} else if u, err := strconv.ParseUint(cell, 10, 64); err == nil {
				if u > t.maxVal {
					t.maxVal = u
				}
			} else if n, err := strconv.ParseInt(cell, 10, 64); err == nil {
				abs := uint64(n)
				if n < 0 {
					abs = uint64(-n)
				}
				if abs > t.maxVal {
					t.maxVal = abs
				}
			} else {
				// digits-only but too big for a 64-bit int; this format's
				// widest int type can't hold it, so fall back to float64
				t.isDecimal = true
			}
		}
	}

	// bool check
	if t.isBoolVals {
		if _, err := strconv.ParseBool(cell); err != nil {
			t.isBoolVals = false
		}
	}

	// track largest cell length in case this column falls back to varchar(N).
	// only fatal once the column can no longer resolve to a fixed-width type
	// (numeric/bool store the parsed value, not the cell's raw bytes, so their
	// length doesn't matter).
	cellLen := len(cell)
	if !t.isNumeric && !t.isBoolVals && cellLen > 255 {
		return errors.New("cell exceeds 255-byte varchar limit")
	}
	if cellLen > 255 {
		cellLen = 255
	}
	if uint8(cellLen) > t.maxCellLen {
		t.maxCellLen = uint8(cellLen)
	}

	return nil
}

// picks a type based on the flags; falls back to a minimal varchar(N) instead of plain string
func (t *TypeTracker) ResolveType() uint8 {
	if t.isNumeric {

		if t.isDecimal {
			return TypeFloat64
		}

		if t.isOnlyPos {
			if t.maxVal <= uint64(MaxUint32Size) {
				return TypeUint32
			}
			return TypeUint64

		} else {
			if t.maxVal <= uint64(MaxInt32Size) {
				return TypeInt32
			}
			return TypeInt64
		}
	} else if t.isBoolVals {
		return TypeBool
	}

	return NewVarcharType(t.maxCellLen)
}

// WriteHeader writes header's fields to filename in the .jdb binary format.
func WriteHeader(header Header, filename string) error {

	// Open file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write Magic, Version, and ColumnCount
	err = binary.Write(file, binary.LittleEndian, header.Magic)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, header.Version)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, header.ColumnCount)
	if err != nil {
		return err
	}

	// Row count
	err = binary.Write(file, binary.LittleEndian, header.RowCount)
	if err != nil {
		return err
	}

	// Column Loop
	for _, col := range header.Columns {
		err = binary.Write(file, binary.LittleEndian, col.Offset)
		if err != nil {
			return err
		}
		err = binary.Write(file, binary.LittleEndian, col.Type)
		if err != nil {
			return err
		}
		nameBytes := []byte(col.Name)
		err = binary.Write(file, binary.LittleEndian, uint8(len(nameBytes)))
		if err != nil {
			return err
		}
		err = binary.Write(file, binary.LittleEndian, nameBytes)
		if err != nil {
			return err
		}
	}

	return nil
}

// TestType returns nil if a cell is successfully converted
func TestType(colType uint8, cell string) error {
	if IsVarchar(colType) {
		if len(cell) > int(VarcharMaxLen(colType)) {
			return errors.New("cell exceeds varchar max length")
		}
		return nil
	}

	var err error
	switch colType {
	case TypeInt32:
		_, err = strconv.ParseInt(cell, 10, 32)
	case TypeInt64:
		_, err = strconv.ParseInt(cell, 10, 64)
	case TypeUint32:
		_, err = strconv.ParseUint(cell, 10, 32)
	case TypeUint64:
		_, err = strconv.ParseUint(cell, 10, 64)
	case TypeFloat64:
		_, err = strconv.ParseFloat(cell, 64)
	case TypeDate:
		// stored as unix timestamp (int64)
		_, err = strconv.ParseInt(cell, 10, 64)
	case TypeBool:
		_, err = strconv.ParseBool(cell)
	case TypeNumeric:
		_, err = strconv.ParseFloat(cell, 64)
	default:
		err = errors.New("Passed type is invalid")
	}

	return err
}

// WriteCSV reads a CSV file at csvFilename and converts it into a JackyDB
// binary columnar (.jdb) file.
//
// If jdbFilename is empty, the output filename is derived from csvFilename
// (".csv" replaced with ".jdb"). Otherwise the given jdbFilename is used.
//
// colTypes optionally specifies the type of each column, in column order,
// using string names ("int32", "int64", "uint32", "uint64", "float64",
// "bool", "date", "numeric"), or a sized varchar via "varchar(N)" /
// "varcharN" (1-255; N<=9 collides with the type enum and silently
// becomes varchar(32), see NewVarcharType). If colTypes is nil, column
// types are inferred automatically from the CSV data. If a given
// colTypes value fails to parse for any row in a column, that column
// falls back to varchar.
//
// WriteCSV performs two passes over the CSV: the first determines row
// count, resolves column types, and computes column byte offsets; the
// second streams rows into the .jdb file's data section.
//
// It returns the resulting Header on success, or an error if the CSV
// could not be read or the .jdb file could not be written.
func WriteCSV(csvFilename string, jdbFilename string, colTypes []string) (Table, error) {

	file, err := os.Open(csvFilename)
	if err != nil {
		return Table{}, err
	}
	defer file.Close()

	head := Header{}
	table := Table{}
	reader := csv.NewReader(file)

	// FIRST PASS
	// -------------------------------------------------------------
	// header initialization (first row)
	row, err := reader.Read()
	if err != nil {
		return table, err // if first row is empty, file is invalid
	}
	head.Magic = MagicConst
	head.Version = CurrentVersion
	head.ColumnCount = uint16(len(row))
	if head.ColumnCount == 0 {
		return Table{}, errors.New("No valid columns")
	}
	if colTypes != nil && len(colTypes) != int(head.ColumnCount) {
		return Table{}, fmt.Errorf("colTypes length (%d) does not match column count (%d)", len(colTypes), head.ColumnCount)
	}
	head.RowCount = 0
	head.Columns = make([]ColumnMeta, head.ColumnCount)
	columnTypes := make([]TypeTracker, head.ColumnCount)
	isTypePassed := make([]bool, head.ColumnCount) // indexed by column position, not name (names may repeat)

	// individual column metadata
	for i, col := range row {

		// offset
		head.Columns[i].Offset = 0 // temp; to be updated after first pass

		// name
		head.Columns[i].Name = col

		// length
		if len(col) > 255 {
			return Table{}, fmt.Errorf("column name %q exceeds 255-byte limit", col)
		}
		head.Columns[i].Length = uint8(len(col))

		// type
		if colTypes != nil {
			tag, ok := stringToType[colTypes[i]]
			if !ok {
				tag, ok = ParseVarcharType(colTypes[i])
			}
			if !ok {
				isTypePassed[i] = false
			} else {
				isTypePassed[i] = true
				head.Columns[i].Type = tag // byte enum
			}
		}

		// always track flags; if the explicit type fails later
		// have full history from row 1
		columnTypes[i] = NewTypeTracker()
	}

	// finalizing header
	for {
		row, err = reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Table{}, err
		}
		head.RowCount++

		// per row scans (going left to right; horizontal)
		for i, col := range row {
			// type handling
			if isTypePassed[i] {
				err = TestType(head.Columns[i].Type, col)
				if err != nil {
					isTypePassed[i] = false // cell failed parsing; columnTypes[i] already has full history from row 1
				}
			}
			// always update flags so the tracker stays accurate even for passed types
			if err := columnTypes[i].UpdateFlags(col); err != nil {
				return Table{}, fmt.Errorf("column %q: %w", head.Columns[i].Name, err)
			}
		}
	}

	// determine types
	// if type is explicit and all cells 'passed', keep explicit type
	for i, col := range columnTypes {
		if !isTypePassed[i] {
			head.Columns[i].Type = col.ResolveType()
		}
	}

	// determine offset
	var totalNameLength int
	for _, col := range head.Columns {
		totalNameLength += int(col.Length)
	}
	offset := 5 + 1 + 2 + 8 + totalNameLength + (10 * int(head.ColumnCount)) // headersize initally; beginning of data bytes
	for i := range head.Columns {
		head.Columns[i].Offset = uint64(offset)
		offset += int(head.RowCount) * ByteSizeForType(head.Columns[i].Type)
	}
	// -------------------------------------------------------------

	// SECOND PASS
	// -------------------------------------------------------------

	// -------------------------------------------------------------

	table.Head = head
	return table, nil

}
