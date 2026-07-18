package main

import (
	"encoding/binary"
	"os"
)

func WriteHeader(header Header, filename string) error {

	// Open file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write Magic and ColumnCount
	err = binary.Write(file, binary.LittleEndian, header.Magic)
	if err != nil {
		return err
	}
	err = binary.Write(file, binary.LittleEndian, header.ColumnCount)
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

	// Row count
	err = binary.Write(file, binary.LittleEndian, header.Rows)

	return nil
}
