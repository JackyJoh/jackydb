package main

import (
	"encoding/binary"
	"os"
)

func ReadHeader(filename string) (Header, error) {

	file, err := os.Open(filename)
	if err != nil {
		return Header{}, err
	}
	defer file.Close()

	// magic
	var mag [5]byte
	err = binary.Read(file, binary.LittleEndian, &mag)
	if err != nil {
		return Header{}, err
	}

	// version
	var version uint8
	err = binary.Read(file, binary.LittleEndian, &version)
	if err != nil {
		return Header{}, err
	}

	// colCount
	var colCount uint16
	err = binary.Read(file, binary.LittleEndian, &colCount)
	if err != nil {
		return Header{}, err
	}

	// rowCount
	var rowCount uint64
	err = binary.Read(file, binary.LittleEndian, &rowCount)
	if err != nil {
		return Header{}, err
	}

	// columns metadata
	colMeta := make([]ColumnMeta, 0)
	for range colCount {
		var colOff uint64
		err = binary.Read(file, binary.LittleEndian, &colOff)
		if err != nil {
			return Header{}, err
		}
		var hasNulls bool
		err = binary.Read(file, binary.LittleEndian, &hasNulls)
		if err != nil {
			return Header{}, err
		}
		var colType uint8
		err = binary.Read(file, binary.LittleEndian, &colType)
		if err != nil {
			return Header{}, err
		}
		var colLength uint8
		err = binary.Read(file, binary.LittleEndian, &colLength)
		if err != nil {
			return Header{}, err
		}
		colName := make([]byte, int(colLength))
		err = binary.Read(file, binary.LittleEndian, &colName)
		if err != nil {
			return Header{}, err
		}
		tempCol := ColumnMeta{
			Offset:   colOff,
			HasNulls: hasNulls,
			Type:     colType,
			Length:   colLength,
			Name:     string(colName),
		}
		colMeta = append(colMeta, tempCol)
	}

	head := Header{
		Magic:       mag,
		Version:     version,
		ColumnCount: colCount,
		RowCount:    rowCount,
		Columns:     colMeta,
	}

	return head, nil
}
