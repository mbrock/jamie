package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/viper"
)

//go:embed db_init.sql
var sqlFS embed.FS

func OpenDatabase() (*pgx.Conn, *Queries, error) {
	sqlDB, err := pgx.Connect(
		context.Background(),
		viper.GetString("DATABASE_URL")+"?sslmode=disable",
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	queries := New(sqlDB)

	sqlFile, err := sqlFS.ReadFile("db_init.sql")
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to read embedded db_init.sql: %w",
			err,
		)
	}

	_, err = sqlDB.Exec(context.Background(), string(sqlFile))
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to execute embedded db_init.sql: %w",
			err,
		)
	}

	return sqlDB, queries, nil
}
