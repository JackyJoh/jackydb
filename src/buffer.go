package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// enum for 'areas' of data in the .jdb file post header
type regionType int

const (
	regionBitmap    regionType = 0
	regionOffsetMap regionType = 1
	regionData      regionType = 2
)

type ColumnWriters struct {
	// 3 writers per column (nil if column does not include that region)
	nbmWriter       *RegionWriter // for bitmap of nulls
	offsetMapWriter *RegionWriter // for offset map of variable-length data
	dataWriter      *RegionWriter // for actual column data
}

type RegionWriter struct {
	// a writer for a specific region of a column
	bw      *bufio.Writer    // buffered writer for this region
	ow      *io.OffsetWriter // tracks the current write offset
	kind    regionType
	curByte uint8  // current byte being written (for bitmap)
	start   uint64 // where this region starts in the file
	end     uint64 // where this region ends in the file (estimated)
}

// newRegionWriter creates a new RegionWriter for a specific region of a column,
// with an appropriate buffer size based on the region type. It initializes the
// buffered writer and offset writer, and sets the start and end offsets for the region.
func newRegionWriter(file *os.File, kind regionType, start uint64, end uint64) *RegionWriter {

	bufSize := 4 * 1024 // 4KB buffer
	if kind == regionBitmap {
		bufSize = 512 // bitmap writes are small
	}

	ow := io.NewOffsetWriter(file, int64(start))

	return &RegionWriter{
		bw:    bufio.NewWriterSize(ow, bufSize),
		ow:    ow,
		kind:  kind,
		start: start,
		end:   end,
	}
}

// flushAndVerify flushes rw's buffer, then asserts the region ended up
// exactly end-start bytes long.
func (rw *RegionWriter) flushAndVerify() error {
	if err := rw.bw.Flush(); err != nil {
		return err
	}
	pos, err := rw.ow.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if uint64(pos) != rw.end-rw.start {
		return fmt.Errorf("region kind %d [%d,%d): wrote %d bytes, expected %d", rw.kind, rw.start, rw.end, pos, rw.end-rw.start)
	}
	return nil
}
