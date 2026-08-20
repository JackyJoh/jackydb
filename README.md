# JackyDB

A read-only columnar storage engine and SQL query executor, written in Go.

## MVP

- Load a CSV, convert to a custom binary columnar format (`.jdb`)
- Execute a SQL subset against `.jdb` files: `SELECT`, `WHERE`, `GROUP BY` + aggregates, `ORDER BY`
- Benchmark against raw CSV and DuckDB

## `.jdb` file format

```
magic      : 5
version    : 1
col_count  : 2
row_count  : 8
--------------------
offset      : 8
has_nulls   : 1
type        : 1
name_len    : 1
name        : name_len
(repeat per column)
--------------------
per column, fixed-width types: [null bitmap (if any)][raw column data]
per column, TypeString:        [null bitmap (if any)][offset map][entrySize marker][blob]
per column, decimal:           [null bitmap (if any)][preciscion][scale][blob]

"offset" always points at the start of the last bracket (raw data / blob).
everything before it is derived backwards from "offset", using row_count
and (for TypeString) entrySize.
```