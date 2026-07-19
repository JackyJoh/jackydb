package main

import "fmt"

func main() {

	var testFile = "../testfile.jdb"
	testHeader := Header{Magic: [5]byte{'j', 'a', 'c', 'k', 'y'}, ColumnCount: 0x02, RowCount: 0x00,
		Columns: []ColumnMeta{
			{Offset: 0, Type: 0x01, Length: 8, Name: "order_id"},
			{Offset: 4, Type: 0x07, Length: 10, Name: "customer"},
		},
	}

	err := WriteHeader(testHeader, testFile)
	if err != nil {
		fmt.Println("error:", err)
	}

	readHeader, err := ReadHeader(testFile)
	if err != nil {
		fmt.Println("error:", err)
	}

	fmt.Printf("Written Magic: %s | Read Magic: %s\n", testHeader.Magic, readHeader.Magic)
	fmt.Printf("Written ColumnCount: %d | Read ColumnCount: %d\n", testHeader.ColumnCount, readHeader.ColumnCount)
	fmt.Printf("Written RowCount: %d | Read RowCount: %d\n", testHeader.RowCount, readHeader.RowCount)

	for i, col := range readHeader.Columns {
		fmt.Printf("Read Column %d: Offset=%d Type=%d Length=%d Name=%s\n",
			i, col.Offset, col.Type, col.Length, col.Name)
	}
}
