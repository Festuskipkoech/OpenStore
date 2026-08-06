package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "modernc.org/sqlite"
)

const usage = `
Usage:
  go run scripts/migrate/main.go <command> [args]

Commands:
  up Apply all pending migrations
  down <n> Roll back n migrations (e.g. down 1)
  version Print current applied migration version
  force <n> Force set version to n (use after fixing a dirty migration)

Environment:
  OPENSTORE_DB_PATH Path to sqlite db file (default: ./openstore.db)
  OPENSTORE_MIGRATIONS_PATH Path to migrations dir default: internal/db/migrations)

Examples:
  go run scripts/migrate/main.go up
  go run scripts/migrate/main.go down 1
  go run scripts/migrate/main.go version
  go run scripts/migrate/main.go force 2
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	dbPath := os.Getenv("OPENSTORE_DB_PATH")
	if dbPath == "" {
		dbPath = "./openstore.db"
	}

	migrationsPath := os.Getenv("OPENSTORE_MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "internal/db/migrations"
	}

	m, err := newMigrate(dbPath, migrationsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			fmt.Fprintf(os.Stderr, "up failed: %v\n", err)
			os.Exit(1)
		}
		printVersion(m)

	case "down":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "down requires a step count, e.g. down 1\n")
			os.Exit(1)
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "step count must be a positive integer\n")
			os.Exit(1)
		}
		if err := m.Steps(-n); err != nil && err != migrate.ErrNoChange {
			fmt.Fprintf(os.Stderr, "down failed: %v\n", err)
			os.Exit(1)
		}
		printVersion(m)

	case "version":
		printVersion(m)

	case "force":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "force requires a version number, e.g. force 2\n")
			os.Exit(1)
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n < 0 {
			fmt.Fprintf(os.Stderr, "version must be a non-negative integer\n")
			os.Exit(1)
		}
		if err := m.Force(n); err != nil {
			fmt.Fprintf(os.Stderr, "force failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("forced to version %d\n", n)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Print(usage)
		os.Exit(1)
	}
}

func newMigrate(dbPath, migrationsPath string) (*migrate.Migrate, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("create driver: %w", err)
	}

	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve migrations path: %w", err)
	}

	sourceURL := fmt.Sprintf("file://%s", absPath)

	m, err := migrate.NewWithDatabaseInstance(sourceURL, "sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrate: %w", err)
	}

	return m, nil
}

func printVersion(m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if err == migrate.ErrNilVersion {
		fmt.Println("version: none (no migrations applied)")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read version: %v\n", err)
		return
	}
	if dirty {
		fmt.Printf("version: %d (dirty - migration failed partway, run force to fix)\n", version)
	} else {
		fmt.Printf("version: %d\n", version)
	}
}