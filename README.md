# witness

An audit log for Go, modelled on [django-auditlog](https://github.com/jazzband/django-auditlog).
Register your models, and every create, update and delete is recorded with a field-level diff,
the actor, and the remote address. Adapters for **GORM**, **Bun** and **Ent**.

```
create users#1 by alice  {"Name":{"old":null,"new":"Bob"},"Password":{"old":null,"new":"****"}}
update users#1 by alice  {"Name":{"old":"Bob","new":"Robert"}}
delete users#1 by alice  {"Name":{"old":"Robert","new":null},"Password":{"old":"****","new":null}}
```

## Install

```bash
go get github.com/shubhodeep9/witness            # core
go get github.com/shubhodeep9/witness/gormaudit   # and/or bunaudit, entaudit
```

## Packages

Each adapter is its own Go module, so you only pull in the ORM you use.

| Import path | What | Go |
|---|---|---|
| `github.com/shubhodeep9/witness` | Core: `Entry`, `Diff`, `Registry`, `Store`, context metadata. No dependencies. | 1.23 |
| `github.com/shubhodeep9/witness/store/memory` | In-memory `Store` for tests and examples | 1.23 |
| `github.com/shubhodeep9/witness/middleware` | `net/http` middleware that sets the actor and remote address | 1.23 |
| `github.com/shubhodeep9/witness/gormaudit` | GORM plugin | 1.23 |
| `github.com/shubhodeep9/witness/bunaudit` | Bun query hook (Bun v1.2.16+, see [Bun versions](#bun-versions)) | 1.24 |
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

> **Wrap writes in a transaction.** GORM is atomic by default. With Bun and Ent, a write made
> *outside* a transaction commits first and is audited afterwards, so if the audit write fails
> the change is already saved and its entry is lost. Bun reports the failure to `OnError`; Ent
> returns it to the caller. Inside a transaction a failed audit write rolls everything back.
> See [Atomicity](#atomicity).

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
`Store` against your generated client, using `entaudit.Mutation(ctx).(interface{ Client() *ent.Client })`;
[`examples/ent`](examples/ent) has a complete one with an `AuditLog` schema.

## Atomicity

An audit trail is only trustworthy if a change and its entry succeed or fail together.

| | Outside a transaction | Inside a transaction |
|---|---|---|
| **GORM** | atomic: the plugin runs inside GORM's per-write transaction | atomic |
| **Bun** | the change commits, then the entry is written; if that fails the entry is lost and `Hook.OnError` is called | a failed entry write rolls the transaction back, so `Commit` fails |
| **Ent** | the change commits, then the entry is written; if that fails the error is returned but the change is saved | a failed entry write returns an error; roll back to undo the change |

GORM is only atomic while its default transaction is on: with `SkipDefaultTransaction: true`
a failed entry write returns the error, but the change is already saved (as with Ent outside a
transaction). Bun and Ent can't make the write atomic for you:
a Bun hook runs while the query is already executing on its connection, and an Ent hook has
no generic way to open a transaction. So open one yourself:

```go
// Bun
err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
    _, err := tx.NewUpdate().Model(&user).WherePK().Exec(ctx)
    return err
}) // a failed audit write makes this return an error, and nothing is saved

// Ent
tx, err := client.Tx(ctx)
if err != nil { return err }
if _, err := tx.User.UpdateOneID(id).SetName("Robert").Save(ctx); err != nil {
    return errors.Join(err, tx.Rollback()) // includes a failed audit write
}
return tx.Commit()
```

If some writes can't run in a transaction, make a lost entry loud instead of silent: set
`Hook.OnError` (Bun) to alert or panic, and check the error from every Ent write.

## Behaviour by adapter

| | GORM | Bun | Ent |
|---|---|---|---|
| Old values | extra SELECT before and after an update | extra SELECT before and after an update | extra SELECT per row before; one after an update |
| Bulk update / delete | yes | yes | yes, one entry per row |
| Joins the caller's transaction | yes | yes (reads Bun internals by reflection) | yes (via the generated client) |
| Failed audit write | operation fails and rolls back (with GORM's default transaction; see [Atomicity](#atomicity)) | `OnError` is called and the transaction is rolled back; outside a transaction the change is already committed | operation returns the error; roll back with `client.Tx`; outside a transaction the change is already committed |
| `ObjectType` | table name | table name | Ent type name |

Shared limits: single-column primary keys only, and map-based creates (for example
GORM `Create(&map)`) are not audited.

### Bun versions

`bunaudit` reads some of Bun's internals by reflection, so it is tied to Bun's version. It is
tested with Bun v1.2.16 to v1.2.18 and requires at least v1.2.16; v1.2.15 and earlier don't
compile against it. A CI job (weekly, and on every change to `bunaudit`) runs the full suite
against the newest Bun release. If a Bun upgrade removes what `bunaudit` relies on, writes fail
loudly (`OnError` is called and the transaction rolls back) instead of silently dropping
entries; after upgrading Bun, run `go test ./...` in `bunaudit` (`TestInspect` pins the internals).

## Development

```bash
go test ./...                                  # core; also run in gormaudit/, bunaudit/, entaudit/tests/
(cd entaudit/tests/internal/testent && go generate ./...)   # needed before testing entaudit (cd entaudit/tests)
(cd examples/ent/ent && go generate ./...)            # needed before building the Ent example

# The adapter tests run on in-memory SQLite. To run them on Postgres, point WITNESS_TEST_PG at a server;
# each test gets its own schema:
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16
WITNESS_TEST_PG='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./...
```

The Ent code under `entaudit/tests/internal/testent` and `examples/ent/ent` is generated and
gitignored; only the schemas and `generate.go` files are committed. The SQLite tests use cgo.

## Releasing

Each module is versioned separately. Adapters depend on a released core version, not the
local one, so release the core first:

1. Tag the core (`git tag v0.x.y`) and push the tag.
2. In each adapter, `go get github.com/shubhodeep9/witness@v0.x.y && go mod tidy`; commit.
3. Tag each adapter with its directory prefix (`gormaudit/v0.x.y`, `bunaudit/v0.x.y`,
   `entaudit/v0.x.y`) and push the tags.

`entaudit/tests` and `examples/` are never released and keep `replace` directives so they build against the local source. `entaudit/tests` is its own module so that
the published `entaudit` carries no test files that import generated (unpublished) code.
