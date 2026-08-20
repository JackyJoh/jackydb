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
	isNumeric     bool
	isDecimal     bool
	isFloat32Safe bool // true if every decimal cell so far round-trips exactly through float32
	isDecimalSafe bool // true if no cell so far has used scientific notation (e/E)
	isOnlyPos     bool
	maxVal        uint64 // use isOnlyPos for signs
	isBoolVals    bool
	maxIntDigits  uint8  // largest digit count seen before '.' (or whole cell if no '.'); precision = maxIntDigits + maxScale
	maxScale      uint8  // largest count of digits after '.' seen; used for TypeDecimal scale
	hasNulls      bool   // true once an empty cell has been seen for this column
	bytesSeen     uint64 // tracks the amount of bytes the data blob consumes
	// 						 used for entrySize calc if TypeString
}

// Flags start "on" (true) and get flipped "off" (false) as violating cells are seen
func NewTypeTracker() TypeTracker {
	return TypeTracker{
		isNumeric:     true,
		isDecimal:     false,
		isFloat32Safe: true,
		isDecimalSafe: true,
		isOnlyPos:     true,
		maxVal:        0,
		isBoolVals:    true,
		maxIntDigits:  0,
		maxScale:      0,
		hasNulls:      false,
	}
}

// UpdateFlags inspects a single cell for a column and flips tracker flags off
// flags only move one-way (true -> false); flags cannot be re-enabled.
// Returns an error if the cell can't be represented at all (exceeds the
// 255-byte cap of the maxCellLen tracker field).
func (t *TypeTracker) UpdateFlags(cell string) error {
	// empty cell = null; doesn't count as evidence for or against any type
	if cell == "" {
		t.hasNulls = true
		return nil
	}

	// numeric check (covers int/uint/float/decimal)
	if t.isNumeric {
		f, err := strconv.ParseFloat(cell, 64)
		if err != nil {
			t.isNumeric = false
		} else {
			if f < 0 {
				t.isOnlyPos = false
			}
			// track largest digit count before '.' (or the whole cell if no
			// '.') seen; combined with maxScale this gives TypeDecimal's
			// needed precision. computed on the int part only - a plain
			// integer cell like "500" needs its digits counted here even
			// though some other cell in the column is what sets maxScale,
			// since "500" still needs padding out to that scale later
			intPart := cell
			if dotIdx := strings.Index(cell, "."); dotIdx != -1 {
				intPart = cell[:dotIdx]
			}
			intDigits := uint8(len(intPart)) - uint8(strings.Count(intPart, "-")) - uint8(strings.Count(intPart, "+")) - uint8(strings.Count(intPart, "e")) - uint8(strings.Count(intPart, "E"))
			if intDigits > t.maxIntDigits {
				t.maxIntDigits = intDigits
			}

			// maxVal must come from an exact integer parse, not  float64
			if strings.ContainsAny(cell, ".eE") {
				t.isDecimal = true
				// (e/E) means the digit count we're tracking
				// no longer reflects real magnitude; decimal can't be
				// trusted for this column (use float instead)
				if strings.ContainsAny(cell, "eE") {
					t.isDecimalSafe = false
				}
				// track largest count of digits after '.' seen; used to
				// determine TypeDecimal scale
				if dotIdx := strings.Index(cell, "."); dotIdx != -1 {
					scale := uint8(len(cell) - dotIdx - 1)
					if scale > t.maxScale {
						t.maxScale = scale
					}
				}
				// float32-safe only if the value still prints the same when
				// rounded to float32 precision; (float32 and float64
				// round "1.8" to different bit patterns even though both
				// still mean 1.8)
				if t.isFloat32Safe {
					f32, err32 := strconv.ParseFloat(cell, 32)
					if err32 != nil || strconv.FormatFloat(f32, 'g', -1, 32) != strconv.FormatFloat(f, 'g', -1, 64) { // round-trip check
						t.isFloat32Safe = false
					}
				}
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

	t.bytesSeen += uint64(len(cell))

	return nil
}

// picks a type based on the flags; falls back to TypeString if the column
// is neither numeric nor bool
func (t *TypeTracker) ResolveType() uint8 {
	if t.isNumeric {

		if t.isDecimal {
			if t.isDecimalSafe {
				// precision = int-part digits + scale; check it fits the cap
				if uint16(t.maxIntDigits)+uint16(t.maxScale) <= uint16(MaxDecimalPrecision) {
					return TypeDecimal
				}
			}
			if t.isFloat32Safe {
				return TypeFloat32
			}
			return TypeFloat64
		}

		if t.isOnlyPos {
			if t.maxVal <= uint64(MaxUint8Size) {
				return TypeUint8
			}
			if t.maxVal <= uint64(MaxUint16Size) {
				return TypeUint16
			}
			if t.maxVal <= uint64(MaxUint32Size) {
				return TypeUint32
			}
			return TypeUint64

		} else {
			if t.maxVal <= uint64(MaxInt8Size) {
				return TypeInt8
			}
			if t.maxVal <= uint64(MaxInt16Size) {
				return TypeInt16
			}
			if t.maxVal <= uint64(MaxInt32Size) {
				return TypeInt32
			}
			return TypeInt64
		}
	} else if t.isBoolVals {
		return TypeBool
	}

	return TypeString
}

// WriteHeader writes header's fields to file in the .jdb binary format.
func WriteHeader(header Header, file *os.File) error {

	// Write Magic, Version, and ColumnCount
	err := binary.Write(file, binary.LittleEndian, header.Magic)
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
		err = binary.Write(file, binary.LittleEndian, col.HasNulls)
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
	if cell == "" {
		return nil // empty cell = null; always valid regardless of declared type
	}

	if colType == TypeString {
		return errors.New("TypeString encoding not yet implemented")
	}

	var err error
	switch colType {
	case TypeInt8:
		_, err = strconv.ParseInt(cell, 10, 8)
	case TypeInt16:
		_, err = strconv.ParseInt(cell, 10, 16)
	case TypeInt32:
		_, err = strconv.ParseInt(cell, 10, 32)
	case TypeInt64:
		_, err = strconv.ParseInt(cell, 10, 64)
	case TypeUint8:
		_, err = strconv.ParseUint(cell, 10, 8)
	case TypeUint16:
		_, err = strconv.ParseUint(cell, 10, 16)
	case TypeUint32:
		_, err = strconv.ParseUint(cell, 10, 32)
	case TypeUint64:
		_, err = strconv.ParseUint(cell, 10, 64)
	case TypeFloat32:
		_, err = strconv.ParseFloat(cell, 32)
	case TypeFloat64:
		_, err = strconv.ParseFloat(cell, 64)
	case TypeDate:
		// stored as unix timestamp (int64)
		_, err = strconv.ParseInt(cell, 10, 64)
	case TypeBool:
		_, err = strconv.ParseBool(cell)
	default:
		err = errors.New("Passed type is invalid")
	}

	return err
}

// writeBitmap sets the null bit for rowIdx in the bitmap starting at
// bitmapOffset. One bit per row, so no type parameter is needed.
func writeBitmap(file *os.File, bitmapOffset uint64, rowIdx int) error {
	byteIdx := rowIdx / 8
	bitIdx := rowIdx % 8

	b := make([]byte, 1)
	if _, err := file.ReadAt(b, int64(bitmapOffset+uint64(byteIdx))); err != nil {
		return err
	}
	b[0] |= 1 << bitIdx
	_, err := file.WriteAt(b, int64(bitmapOffset+uint64(byteIdx)))
	return err
}

// writeGeneric writes a single fixed-width cell: every type with no extra
// pre-offset structure (ints, uints, floats, bool, date)
func writeGeneric(file *os.File, col ColumnMeta, rowIdx int, cell string) error {
	byteCount := ByteSizeForType(col.Type)
	dataOffset := col.Offset + uint64(rowIdx)*uint64(byteCount)

	if cell == "" && col.HasNulls {
		_, err := file.WriteAt(make([]byte, byteCount), int64(dataOffset))
		return err
	}

	dataBytes, err := EncodeCell(col.Type, cell)
	if err != nil {
		return err
	}
	_, err = file.WriteAt(dataBytes, int64(dataOffset))
	return err
}

// encodeOffsetEntry returns v as a little-endian byte slice of the given
// entrySize width (1, 2, 4, or 8 bytes), matching EntrySizeForBlobLen.
func encodeOffsetEntry(v uint64, entrySize uint8) []byte {
	buf := make([]byte, entrySize)
	switch entrySize {
	case 1:
		buf[0] = uint8(v)
	case 2:
		binary.LittleEndian.PutUint16(buf, uint16(v))
	case 4:
		binary.LittleEndian.PutUint32(buf, uint32(v))
	case 8:
		binary.LittleEndian.PutUint64(buf, v)
	}
	return buf
}

// decodeOffsetEntry reads a little-endian offset-map entry of the given
// entrySize width (1, 2, 4, or 8 bytes) back into a uint64.
func decodeOffsetEntry(b []byte, entrySize uint8) uint64 {
	switch entrySize {
	case 1:
		return uint64(b[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(b))
	case 4:
		return uint64(binary.LittleEndian.Uint32(b))
	default:
		return binary.LittleEndian.Uint64(b)
	}
}

// writeString writes a single TypeString cell: it reads the row's starting
// blob offset back from the offset map (already on disk from the previous
// row's write, or zero-filled by Truncate for row 0), writes the cell's
// bytes into the blob at that position, then advances the offset map by
// len(cell) for the next row to read. The entrySize marker itself is
// written once per column before the row loop starts, not here.
func writeString(file *os.File, col ColumnMeta, rowIdx int, cell string, entrySize uint8, rowCount uint64) error {
	offsetMapStart := col.StringOffsetMapOffset(rowCount, entrySize)

	// read the previous row's starting offset
	prevBytes := make([]byte, entrySize)
	if _, err := file.ReadAt(prevBytes, int64(offsetMapStart+uint64(rowIdx)*uint64(entrySize))); err != nil {
		return err
	}
	prevOffset := decodeOffsetEntry(prevBytes, entrySize)

	// write the cell's bytes into the blob at the previous row's offset
	// update the next row's offset to be the previous offset + len(cell)
	nextOffset := prevOffset
	if cell == "" && col.HasNulls {
		// null cell: offsetMap[rowIdx+1] = offsetMap[rowIdx] + 0, no blob bytes written
	} else {
		blobPos := col.Offset + prevOffset
		if _, err := file.WriteAt([]byte(cell), int64(blobPos)); err != nil {
			return err
		}
		nextOffset += uint64(len(cell))
	}

	// write the next row's starting offset into the offset map
	nextEntryOffset := offsetMapStart + uint64(rowIdx+1)*uint64(entrySize)
	_, err := file.WriteAt(encodeOffsetEntry(nextOffset, entrySize), int64(nextEntryOffset))
	return err
}

// precisionToByteSize returns the byte width needed to hold a TypeDecimal
// mantissa of the given precision (digit count), same tiers used in the
// offset-calc loop: <=2 -> int8, <=4 -> int16, <=9 -> int32, <=18 -> int64.
func precisionToByteSize(precision uint8) int {
	switch {
	case precision <= 2:
		return 1
	case precision <= 4:
		return 2
	case precision <= 9:
		return 4
	default:
		return 8
	}
}

// encodeDecimalMantissa parses a decimal-point-stripped digit string (e.g.
// "-314" for original cell "-3.14") and encodes it as a little-endian signed
// integer of the given byte width (1, 2, 4, or 8, from precisionToByteSize).
func encodeDecimalMantissa(cell string, byteWidth int) ([]byte, error) {
	v, err := strconv.ParseInt(cell, 10, byteWidth*8)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, byteWidth)
	switch byteWidth {
	case 1:
		buf[0] = uint8(v)
	case 2:
		binary.LittleEndian.PutUint16(buf, uint16(v))
	case 4:
		binary.LittleEndian.PutUint32(buf, uint32(v))
	case 8:
		binary.LittleEndian.PutUint64(buf, uint64(v))
	}
	return buf, nil
}

// writeDecimal writes a single TypeDecimal cell: pads the fractional digits
// out to the column's scale, strips the '.', then encodes the resulting
// digit string as a fixed-width signed integer mantissa.
func writeDecimal(file *os.File, col ColumnMeta, rowIdx int, cell string, precision uint8, scale uint8) error {
	byteWidth := precisionToByteSize(precision)
	dataOffset := col.Offset + uint64(rowIdx)*uint64(byteWidth)

	if cell == "" && col.HasNulls {
		_, err := file.WriteAt(make([]byte, byteWidth), int64(dataOffset))
		return err
	}

	// add padding to meet scale. works for both integer and decimal cells.
	afterDot := strings.Index(cell, ".")
	existingScale := 0
	if afterDot != -1 {
		existingScale = len(cell) - afterDot - 1
	}
	for i := existingScale; i < int(scale); i++ {
		cell += "0"
	}
	// strip cell of '.'
	cell = strings.ReplaceAll(cell, ".", "")

	// encode and write cell as a little-endian signed integer of the appropriate byte width
	dataBytes, err := encodeDecimalMantissa(cell, byteWidth)
	if err != nil {
		return err
	}
	_, err = file.WriteAt(dataBytes, int64(dataOffset))
	return err
}

// WriteCSV reads a CSV file at csvFilename and converts it into a JackyDB
// binary columnar (.jdb) file.
//
// If jdbFilename is empty, the output filename is derived from csvFilename
// (".csv" replaced with ".jdb"). Otherwise the given jdbFilename is used.
//
// colTypes optionally specifies the type of each column, in column order,
// using string names ("int8", "int16", "int32", "int64", "uint8", "uint16",
// "uint32", "uint64", "float32", "float64", "bool", "date", "numeric",
// "string"). If colTypes is nil, column types are inferred automatically from
// the CSV data. If a given colTypes value fails to parse for any row in a
// column, that column falls back to TypeString.
//
// WriteCSV performs two passes over the CSV: the first determines row
// count, resolves column types, and computes column byte offsets; the
// second streams rows into the .jdb file's data section.
//
// It returns the resulting Table on success, or an error if the CSV
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

		// error if row has more cols than header
		if len(row) != int(head.ColumnCount) {
			return Table{}, fmt.Errorf("row contains more columns than header: %q", row)
		}

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
	offset := 5 + 1 + 2 + 8 + totalNameLength + (11 * int(head.ColumnCount)) // headersize initally; beginning of data bytes
	// entrySize per column; only meaningful where Type == TypeString. Computed
	// once here (from bytesSeen, already known from the first pass) so the
	// second pass never needs to recompute or re-derive it.
	entrySizes := make([]uint8, head.ColumnCount)
	for i := range head.Columns {
		// bitmap (if any) goes immediately before this column's data
		head.Columns[i].HasNulls = columnTypes[i].hasNulls
		if head.Columns[i].HasNulls {
			bitmapSize := int((head.RowCount + 7) / 8) // ceil(RowCount / 8)
			offset += bitmapSize
		}
		if head.Columns[i].Type == TypeString {
			// [null bitmap][offset map: (RowCount+1) entries][entrySize marker, 1 byte][string blob]
			// Offset always points at the blob start; the marker and offset
			// map are derived backwards from Offset (see StringEntrySizeOffset /
			// StringOffsetMapOffset), the same way NullBitmapOffset derives
			// backwards from Offset for fixed-width columns.
			entrySizes[i] = EntrySizeForBlobLen(columnTypes[i].bytesSeen)
			offsetMapSize := uint64(entrySizes[i]) * (head.RowCount + 1)
			offset += int(offsetMapSize) + 1
			head.Columns[i].Offset = uint64(offset)
			offset += int(columnTypes[i].bytesSeen)
		} else if head.Columns[i].Type == TypeDecimal {
			offset += 2 // precision and scale
			head.Columns[i].Offset = uint64(offset)
			precision := columnTypes[i].maxIntDigits + columnTypes[i].maxScale
			cellSize := precisionToByteSize(precision)
			offset += int(head.RowCount) * cellSize
		} else {
			head.Columns[i].Offset = uint64(offset)
			offset += int(head.RowCount) * ByteSizeForType(head.Columns[i].Type)
		}
	}
	// -------------------------------------------------------------

	// create jdb file and point table at file
	if jdbFilename == "" {
		jdbFilename = strings.TrimSuffix(csvFilename, ".csv") + ".jdb"
	}
	jdbFile, err := os.Create(jdbFilename)
	if err != nil {
		return Table{}, err
	}
	defer jdbFile.Close()

	table.Head = head
	table.File = jdbFilename

	// write header to jdb
	err = WriteHeader(head, jdbFile)
	if err != nil {
		return Table{}, err
	}

	// pre-allocate the rest of the file (bitmaps + column data) so later
	// ReadAt calls on not-yet-written bitmap bytes don't hit io.EOF
	err = jdbFile.Truncate(int64(offset))
	if err != nil {
		return Table{}, err
	}

	// SECOND PASS
	// -------------------------------------------------------------
	// stream rows into jdb file
	file.Seek(0, io.SeekStart)
	reader = csv.NewReader(file)

	// skip header row
	_, err = reader.Read()
	if err != nil {
		return Table{}, err
	}

	// write each string column's entrySize marker once, up front
	// write each decimal column's precision and scale once, up front
	// per column so it never needs to be touched again during the row loop
	for i := range head.Columns {
		if head.Columns[i].Type == TypeDecimal {
			pOffset := head.Columns[i].DecimalPrecisionOffset()
			_, err = jdbFile.WriteAt([]byte{columnTypes[i].maxIntDigits + columnTypes[i].maxScale}, int64(pOffset))
			if err != nil {
				return Table{}, err
			}
			sOffset := head.Columns[i].DecimalScaleOffset()
			_, err = jdbFile.WriteAt([]byte{columnTypes[i].maxScale}, int64(sOffset))
			if err != nil {
				return Table{}, err
			}
		}
		if head.Columns[i].Type == TypeString {
			markerOffset := head.Columns[i].StringEntrySizeOffset()
			_, err = jdbFile.WriteAt([]byte{entrySizes[i]}, int64(markerOffset))
			if err != nil {
				return Table{}, err
			}
		}
	}

	// row loop
	for rowIdx := 0; ; rowIdx++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Table{}, err
		}
		if rowIdx >= int(head.RowCount) {
			break // extra rows are ignored
		}

		// column loop
		for colIdx, col := range row {
			colMeta := head.Columns[colIdx]

			// if null, set the bit in the bitmap; the dispatched writer below
			// still handles skipping/zero-filling its own data for this cell.
			// TypeString's bitmap sits before the offset map + marker, not
			// immediately before Offset, so it needs its own derivation.
			if col == "" && colMeta.HasNulls {
				var bitmapOffset uint64
				if colMeta.Type == TypeString {
					bitmapOffset = colMeta.StringNullBitmapOffset(head.RowCount, entrySizes[colIdx])
				} else if colMeta.Type == TypeDecimal {
					bitmapOffset = colMeta.DecimalPrecisionOffset() - (head.RowCount+7)/8
				} else {
					bitmapOffset = colMeta.NullBitmapOffset(head.RowCount)
				}
				if err := writeBitmap(jdbFile, bitmapOffset, rowIdx); err != nil {
					return Table{}, err
				}
			}

			switch colMeta.Type {
			case TypeString:
				err = writeString(jdbFile, colMeta, rowIdx, col, entrySizes[colIdx], head.RowCount)
			case TypeDecimal:
				err = writeDecimal(jdbFile, colMeta, rowIdx, col, columnTypes[colIdx].maxIntDigits+columnTypes[colIdx].maxScale, columnTypes[colIdx].maxScale)
			default:
				err = writeGeneric(jdbFile, colMeta, rowIdx, col)
			}
			if err != nil {
				return Table{}, err
			}
		}
	}

	// -------------------------------------------------------------

	return table, nil

}
