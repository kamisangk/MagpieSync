package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"
)

//go:embed schema_v1.sql
var schemaV1 string

//go:embed migrations/*.sql
var migrationFiles embed.FS

func migrateDatabase(ctx context.Context, db *sql.DB) error {
	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, files,
		goose.WithDisableGlobalRegistry(true),
		goose.WithGoMigrations(goose.NewGoMigration(1, &goose.GoFunc{RunTx: applyBaseline}, nil)),
	)
	if err != nil {
		return err
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	sources := provider.ListSources()
	if latest := sources[len(sources)-1].Version; version > latest {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, latest)
	}
	_, err = provider.Up(ctx)
	return err
}

func applyBaseline(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("apply baseline schema: %w", err)
	}

	expected, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer expected.Close()
	expected.SetMaxOpenConns(1)
	if _, err := expected.ExecContext(ctx, schemaV1); err != nil {
		return fmt.Errorf("load baseline schema: %w", err)
	}
	required, err := readBaselineColumns(ctx, expected)
	if err != nil {
		return err
	}
	actual, err := readBaselineColumns(ctx, tx)
	if err != nil {
		return err
	}
	columns := make(map[[2]string]baselineColumn, len(actual))
	for _, column := range actual {
		columns[[2]string{column.table, column.name}] = column
	}
	for _, column := range required {
		got, found := columns[[2]string{column.table, column.name}]
		if !found || got != column {
			return fmt.Errorf("incompatible baseline schema at %s.%s", column.table, column.name)
		}
	}
	return nil
}

type baselineColumn struct {
	table, name, dataType string
	notNull, pk, hidden   int
	defaultValue          sql.NullString
}

func readBaselineColumns(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]baselineColumn, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.name, c.name, c.type, c."notnull", c.pk, c.hidden, c.dflt_value
		FROM main.sqlite_schema AS s
		JOIN pragma_table_xinfo(s.name, 'main') AS c
		WHERE s.type = 'table' AND s.name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY s.name, c.cid`)
	if err != nil {
		return nil, fmt.Errorf("inspect baseline columns: %w", err)
	}
	defer rows.Close()
	var columns []baselineColumn
	for rows.Next() {
		var c baselineColumn
		if err := rows.Scan(&c.table, &c.name, &c.dataType, &c.notNull, &c.pk, &c.hidden, &c.defaultValue); err != nil {
			return nil, err
		}
		c.dataType = strings.ToUpper(c.dataType)
		columns = append(columns, c)
	}
	return columns, rows.Err()
}
