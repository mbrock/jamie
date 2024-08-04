package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/viper"
)

//go:embed db_init.sql
var sqlFS embed.FS

var (
	dbPool   *pgxpool.Pool
	dbQueries *Queries
	dbOnce   sync.Once
)

func OpenDatabase() (*pgxpool.Pool, *Queries, error) {
	var err error
	dbOnce.Do(func() {
		dbPool, err = pgxpool.New(
			context.Background(),
			viper.GetString("DATABASE_URL")+"?sslmode=disable",
		)
		if err != nil {
			err = fmt.Errorf("unable to connect to database: %w", err)
			return
		}

		dbQueries = New(dbPool)

		sqlFile, readErr := sqlFS.ReadFile("db_init.sql")
		if readErr != nil {
			err = fmt.Errorf("failed to read embedded db_init.sql: %w", readErr)
			return
		}

		_, execErr := dbPool.Exec(context.Background(), string(sqlFile))
		if execErr != nil {
			err = fmt.Errorf("failed to execute embedded db_init.sql: %w", execErr)
			return
		}
	})

	if err != nil {
		return nil, nil, err
	}

	return dbPool, dbQueries, nil
}
