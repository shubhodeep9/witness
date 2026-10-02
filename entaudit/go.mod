module github.com/shubhodeep9/witness/entaudit

go 1.25.0

require github.com/shubhodeep9/witness v0.1.0

require entgo.io/ent v0.14.6

retract v0.1.0 // its tests imported generated code that was not published, so go mod tidy failed for importers
