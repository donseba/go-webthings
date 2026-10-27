package db

// Supported database/sql drivers register themselves: sqlite (pure Go, no
// cgo), pgx and mysql.
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)
