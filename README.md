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
per column: [null bitmap (if any)][raw column data]
null bitmap offset = offset - ceil(row_count / 8), when has_nulls
```