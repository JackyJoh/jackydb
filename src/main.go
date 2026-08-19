package main

import "fmt"

func main() {

	testFile := "../test.csv"

	// convert the CSV into a .jdb file, inferring column types automatically
	table, err := WriteCSV(testFile, "", nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// read the header back from the .jdb file WriteCSV just produced
	readHeader, err := ReadHeader(table.File)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Printf("Magic: %s | Version: %d | ColumnCount: %d | RowCount: %d\n",
		readHeader.Magic, readHeader.Version, readHeader.ColumnCount, readHeader.RowCount)

	// print each column's resolved metadata
	for i, col := range readHeader.Columns {
		fmt.Printf("Column %d: Offset=%d HasNulls=%t Type=%s Length=%d Name=%s\n",
			i, col.Offset, col.HasNulls, TypeToString(col.Type), col.Length, col.Name)
	}
}
