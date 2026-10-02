# witness

An audit log for Go, modelled on [django-auditlog](https://github.com/jazzband/django-auditlog).
Register your models, and every create, update and delete is recorded with a field-level diff,
the actor, and the remote address. Adapters for **GORM**, **Bun** and **Ent**.

```
create users#1 by alice  {"Name":{"old":null,"new":"Bob"},"Password":{"old":null,"new":"****"}}
update users#1 by alice  {"Name":{"old":"Bob","new":"Robert"}}
delete users#1 by alice  {"Name":{"old":"Robert","new":null},"Password":{"old":"****","new":null}}
```

## Packages

Each adapter is its own Go module, so you only pull in the ORM you use.

| Import path | What | Go |
|---|---|---|
| `github.com/shubhodeep9/witness` | Core: `Entry`, `Diff`, `Registry`, `Store`, context metadata. No dependencies. | 1.23 |
| `github.com/shubhodeep9/witness/store/memory` | In-memory `Store` for tests and examples | 1.23 |
| `github.com/shubhodeep9/witness/middleware` | `net/http` middleware that sets the actor and remote address | 1.23 |
| `github.com/shubhodeep9/witness/gormaudit` | GORM plugin | 1.23 |
| `github.com/shubhodeep9/witness/bunaudit` | Bun query hook | 1.24 |
| `github.com/shubhodeep9/witness/entaudit` | Ent mutation hook | 1.25 |

## Usage

The steps are the same for every ORM: register models, attach the adapter, and put the
actor in the context.

```go
var reg witness.Registry
reg.Register(&User{}, witness.Options{
    Exclude: []string{"UpdatedAt"}, // never recorded
    Mask:    []string{"Password"},  // recorded as "****"
})

var store memory.Store // or your own witness.Store

db.Use(gormaudit.New(&reg, &store))              // GORM
bunDB.AddQueryHook(bunaudit.New(&reg, &store))   // Bun
client.Use(entaudit.Hook(&reg, &store))          // Ent

ctx = witness.WithMeta(ctx, witness.Meta{Actor: "alice", RemoteAddr: "203.0.113.7"})
db.WithContext(ctx).Create(&user) // audited
```

For HTTP servers, the middleware does the last step for you:

```go
mux := middleware.New(func(r *http.Request) string { return userIDFrom(r) })(handler)
```

It records `r.RemoteAddr` without the port and ignores `X-Forwarded-For`, which clients can
spoof. Behind a proxy, run a real-IP middleware (one that rewrites `r.RemoteAddr`) first.

Runnable versions are in [`examples/`](examples).

- **Only registered models are audited**, as in django-auditlog's `register()`.
- **`Options`** take Go field names: `Include` (empty means all), `Exclude`, `Mask`.
- **`Store`** is one method, `Write(ctx, Entry)`. Implement it to persist entries. Each adapter
  exposes the caller's transaction to the store (`gormaudit.TxFrom`, `bunaudit.Conn`,
  `entaudit.Mutation`) so the audit row can be written atomically.

### Persisting entries

`gormaudit` and `bunaudit` ship a database store that writes a `LogEntry` row in the same
transaction as the change, so the two commit or roll back together:

```go
db.AutoMigrate(&gormaudit.LogEntry{})            // Bun: db.NewCreateTable().Model((*bunaudit.LogEntry)(nil)).Exec(ctx)
db.Use(gormaudit.New(&reg, gormaudit.NewStore())) // Bun: bunaudit.New(&reg, bunaudit.NewStore())

var history []gormaudit.LogEntry
db.Where("object_type = ? AND object_id = ?", "users", "1").Order("id").Find(&history)
```

`LogEntry` is a normal model, so query it with your ORM; `row.Entry()` converts it back to a
`witness.Entry`. Don't register `LogEntry` itself with the `Registry`. For Ent, write your own
`Store` against your generated client, using `entaudit.Mutation(ctx).(interface{ Client() *ent.Client })`.

## Behaviour by adapter

| | GORM | Bun | Ent |
|---|---|---|---|
| Old values | extra SELECT before and after an update | extra SELECT before and after an update | extra SELECT per row before; one after an update |
| Bulk update / delete | yes | yes | yes, one entry per row |
| Joins the caller's transaction | yes | yes (reads Bun internals by reflection) | yes (via the generated client) |
| Failed audit write | operation fails and rolls back | `OnError` is called and the transaction is rolled back; outside a transaction the change is already committed | operation returns the error; roll back with `client.Tx`; outside a transaction the change is already committed |
| `ObjectType` | table name | table name | Ent type name |

Shared limits: single-column primary keys only, and map-based creates (for example
GORM `Create(&map)`) are not audited.

## Development

```bash
go test ./...                                  # core; also run in gormaudit/, bunaudit/, entaudit/
(cd entaudit/internal/testent && go generate ./...)   # needed before testing entaudit
(cd examples/ent/ent && go generate ./...)            # needed before building the Ent example
```

The Ent code under `entaudit/internal/testent` and `examples/ent/ent` is generated and
gitignored; only the schemas and `generate.go` files are committed. The SQLite tests use cgo.

Until the first release, each adapter's `go.mod` points at the core with a `replace`
directive. Before tagging, remove those lines, then tag the core (`v0.1.0`) followed by each
adapter with its directory prefix (`gormaudit/v0.1.0`, `bunaudit/v0.1.0`, `entaudit/v0.1.0`).
