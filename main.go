package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {

	var testFile = "testfile.jdb"
	testHeader := Header{Magic: [5]byte{'j', 'a', 'c', 'k', 'y'}, ColumnCount: 0x02,
		Columns: []ColumnMeta{
			{Offset: 0, Type: 0x01, Length: 8, Name: "order_id"},
			{Offset: 4, Type: 0x07, Length: 10, Name: "customer"},
		},
		Rows: 0x00}

	err := WriteHeader(testHeader, testFile)
	if err != nil {
		fmt.Println("error:", err)
	}

	// read direct from jdb
	readFile, err := os.Open(testFile)
	if err != nil {
		fmt.Println("error:", err)
	}
	defer readFile.Close()

	var magic [5]byte
	err = binary.Read(readFile, binary.LittleEndian, &magic)
	if err != nil {
		fmt.Println("error:", err)
	}

	var colCount uint16
	err = binary.Read(readFile, binary.LittleEndian, &colCount)
	if err != nil {
		fmt.Println("error:", err)
	}

	fmt.Printf("Read Magic: %s\n", magic)
	fmt.Printf("Read ColumnCount: %d\n", colCount)

}
