package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Schema files sit in one folder, named <datetime>[_description].<flavour>.sql:
//
//	schema/20261004000000_init.sqlite.sql
//	schema/20261004000000_init.postgres.sql
//	schema/20261004000000_init.mysql.sql
//	schema/20261120093000_add_avatar.sqlite.sql
//	...
//
// The datetime (YYYYMMDDHHMMSS) is the version. Every version must exist for
// every flavour. ApplySchema runs the current flavour's files that are newer
// than the version recorded for owner in wt_schema, each in a transaction.
//
// Before the first release every owner has only its _init files: edit them
// and recreate the development database. After a release, add new files
// instead of changing applied ones.
//
// wt_schema_step keeps a checksum of each applied file's statements. A file
// whose statements changed after it was applied, also an _init file, makes
// ApplySchema return ErrSchemaChanged: the database does not have what the
// file now says. Changes of only whitespace or comments do not count. A
// database from before the checksums gets those of the files as they are.
func (db *DB) ApplySchema(ctx context.Context, owner string, files fs.FS) error {
	if err := db.ensureLedger(ctx); err != nil {
		return err
	}

	steps, err := schemaSteps(files, db.dialect)
	if err != nil {
		return fmt.Errorf("schema %s: %w", owner, err)
	}

	var current int64

	err = db.Get(ctx, &current, `SELECT version FROM wt_schema WHERE owner=?`, owner)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	applied, err := db.appliedChecksums(ctx, owner)
	if err != nil {
		return err
	}

	for _, step := range steps {
		script, err := fs.ReadFile(files, step.file)
		if err != nil {
			return err
		}

		if step.version <= current {
			if err := db.checkApplied(ctx, owner, step, schemaChecksum(string(script)), applied); err != nil {
				return err
			}

			continue
		}

		if err := db.applyStep(ctx, owner, step, string(script), current == 0); err != nil {
			return err
		}

		current = step.version
	}

	return nil
}

// ErrSchemaChanged is returned by ApplySchema for a schema file whose
// statements changed after it was applied to the database.
var ErrSchemaChanged = errors.New("schema file changed after it was applied")

// checkApplied compares the checksum of an applied file with the one
// recorded when it ran. A file applied before the checksums were kept gets
// its checksum recorded.
func (db *DB) checkApplied(ctx context.Context, owner string, step schemaStep, sum string, applied map[int64]string) error {
	recorded, ok := applied[step.version]
	if !ok {
		return db.recordStep(ctx, owner, step.version, sum)
	}

	if recorded != sum {
		return fmt.Errorf("schema %s: %s: %w; recreate a development database, "+
			"and after a release put changes in a new schema file", owner, step.file, ErrSchemaChanged)
	}

	return nil
}

// appliedChecksums returns the recorded checksum of each applied version of
// owner.
func (db *DB) appliedChecksums(ctx context.Context, owner string) (map[int64]string, error) {
	var rows []struct {
		Version  int64  `db:"version"`
		Checksum string `db:"checksum"`
	}

	err := db.Select(ctx, &rows, `SELECT version, checksum FROM wt_schema_step WHERE owner=?`, owner)
	if err != nil {
		return nil, err
	}

	sums := make(map[int64]string, len(rows))
	for _, row := range rows {
		sums[row.Version] = row.Checksum
	}

	return sums, nil
}

func (db *DB) recordStep(ctx context.Context, owner string, version int64, sum string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO wt_schema_step(owner,version,checksum,recorded_at) VALUES(?,?,?,?)`,
		owner, version, sum, time.Now().UTC(),
	)

	return err
}

// schemaChecksum is the SHA-256 of a schema file's statements, with their
// whitespace collapsed, so layout and comments do not count.
func schemaChecksum(script string) string {
	statements := split(script)
	for i, statement := range statements {
		statements[i] = strings.Join(strings.Fields(statement), " ")
	}

	sum := sha256.Sum256([]byte(strings.Join(statements, ";\n")))

	return hex.EncodeToString(sum[:])
}

type schemaStep struct {
	version int64
	file    string
}

// schemaSteps returns the files for dialect in version order, after checking
// that all flavours have the same versions.
func schemaSteps(files fs.FS, dialect Dialect) ([]schemaStep, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}

	versions := map[Dialect]map[int64]string{SQLite: {}, Postgres: {}, MySQL: {}}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, flavour, err := parseSchemaName(name)
		if err != nil {
			return nil, err
		}

		if _, ok := versions[flavour]; !ok {
			return nil, fmt.Errorf("schema file %s: unknown flavour %q", name, flavour)
		}

		if _, ok := versions[flavour][version]; ok {
			return nil, fmt.Errorf("schema version %d appears twice for %s", version, flavour)
		}

		versions[flavour][version] = name
	}

	wanted := slices.Sorted(maps.Keys(versions[dialect]))

	for flavour, files := range versions {
		if !slices.Equal(wanted, slices.Sorted(maps.Keys(files))) {
			return nil, fmt.Errorf("schema versions for %s differ from %s; add the missing files", flavour, dialect)
		}
	}

	steps := make([]schemaStep, 0, len(wanted))
	for _, version := range wanted {
		steps = append(steps, schemaStep{version: version, file: versions[dialect][version]})
	}

	return steps, nil
}

// parseSchemaName splits "20261004000000_init.postgres.sql".
func parseSchemaName(name string) (int64, Dialect, error) {
	parts := strings.Split(strings.TrimSuffix(name, ".sql"), ".")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("schema file %s: want <datetime>[_description].<flavour>.sql", name)
	}

	stamp, _, _ := strings.Cut(parts[0], "_")

	if _, err := time.Parse("20060102150405", stamp); err != nil {
		return 0, "", fmt.Errorf("schema file %s: name must start with a YYYYMMDDHHMMSS datetime", name)
	}

	version, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return 0, "", err
	}

	return version, Dialect(parts[1]), nil
}

// applyStep runs one schema file and records its version and checksum.
// MySQL commits DDL implicitly, so a failing file there may leave part of
// its changes behind.
func (db *DB) applyStep(ctx context.Context, owner string, step schemaStep, script string, first bool) error {
	return db.Tx(ctx, func(ctx context.Context) error {
		for _, statement := range split(script) {
			if _, err := db.Exec(ctx, statement); err != nil {
				return fmt.Errorf("schema %s %s: %w\n%s", owner, step.file, err, statement)
			}
		}

		if err := db.recordStep(ctx, owner, step.version, schemaChecksum(script)); err != nil {
			return err
		}

		now := time.Now().UTC()

		if first {
			_, err := db.Exec(ctx,
				`INSERT INTO wt_schema(owner,version,applied_at) VALUES(?,?,?)`,
				owner, step.version, now,
			)

			return err
		}

		_, err := db.Exec(ctx,
			`UPDATE wt_schema SET version=?, applied_at=? WHERE owner=?`,
			step.version, now, owner,
		)

		return err
	})
}

func (db *DB) ensureLedger(ctx context.Context) error {
	timestamp := "TIMESTAMP"
	if db.dialect == MySQL {
		timestamp = "DATETIME(6)"
	}

	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS wt_schema (
			owner      VARCHAR(100) PRIMARY KEY,
			version    BIGINT NOT NULL,
			applied_at `+timestamp+` NOT NULL
		)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS wt_schema_step (
			owner       VARCHAR(100) NOT NULL,
			version     BIGINT NOT NULL,
			checksum    VARCHAR(64) NOT NULL,
			recorded_at `+timestamp+` NOT NULL,
			PRIMARY KEY (owner, version)
		)`)

	return err
}

// SchemaVersion is the newest version among the schema files of this
// database's dialect, the version ApplySchema brings an owner to; 0 when
// there are none.
func (db *DB) SchemaVersion(files fs.FS) (int64, error) {
	steps, err := schemaSteps(files, db.dialect)
	if err != nil {
		return 0, err
	}

	if len(steps) == 0 {
		return 0, nil
	}

	return steps[len(steps)-1].version, nil
}

// SchemaVersions returns the applied schema version of every owner, as
// recorded in wt_schema, in one query; readiness checks compare them with
// SchemaVersion.
func (db *DB) SchemaVersions(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Owner   string `db:"owner"`
		Version int64  `db:"version"`
	}

	if err := db.Select(ctx, &rows, `SELECT owner, version FROM wt_schema`); err != nil {
		return nil, err
	}

	versions := make(map[string]int64, len(rows))
	for _, row := range rows {
		versions[row.Owner] = row.Version
	}

	return versions, nil
}
