module github.com/shubhodeep9/witness/examples

go 1.25.0

require (
	github.com/shubhodeep9/witness v0.1.0
	github.com/shubhodeep9/witness/gormaudit v0.0.0
	github.com/uptrace/bun v1.2.18
	github.com/uptrace/bun/dialect/sqlitedialect v1.2.18
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	ariga.io/atlas v0.36.2-0.20250730182955-2c6300d0a3e1 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/bmatcuk/doublestar v1.3.4 // indirect
	github.com/go-openapi/inflect v0.19.0 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/hashicorp/hcl/v2 v2.18.1 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/zclconf/go-cty v1.14.4 // indirect
	github.com/zclconf/go-cty-yaml v1.1.0 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	entgo.io/ent v0.14.6
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/shubhodeep9/witness/bunaudit v0.0.0
	github.com/shubhodeep9/witness/entaudit v0.0.0
	golang.org/x/text v0.21.0 // indirect
)

replace github.com/shubhodeep9/witness => ../

replace github.com/shubhodeep9/witness/gormaudit => ../gormaudit

replace github.com/shubhodeep9/witness/bunaudit => ../bunaudit

replace github.com/shubhodeep9/witness/entaudit => ../entaudit
