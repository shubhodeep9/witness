module github.com/shubhodeep9/witness/examples

go 1.24.0

require (
	github.com/shubhodeep9/witness v0.0.0
	github.com/shubhodeep9/witness/gormaudit v0.0.0
	github.com/uptrace/bun v1.2.18
	github.com/uptrace/bun/dialect/sqlitedialect v1.2.18
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

require (
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/shubhodeep9/witness/bunaudit v0.0.0
	golang.org/x/text v0.20.0 // indirect
)

replace github.com/shubhodeep9/witness => ../

replace github.com/shubhodeep9/witness/gormaudit => ../gormaudit

replace github.com/shubhodeep9/witness/bunaudit => ../bunaudit
