package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"domain-platform/internal/bootstrap"
)

const migrationsPath = "file://migrations"

func main() {
	cfg, err := bootstrap.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	m, err := migrate.New(migrationsPath, cfg.DB.URL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "init migrate: %v\n", err)
		os.Exit(1)
	}
	defer m.Close()

	cmd := os.Args[1]
	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fmt.Fprintf(os.Stderr, "migrate up: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migrate up: done")

	case "down":
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fmt.Fprintf(os.Stderr, "migrate down: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migrate down: done")

	case "version":
		version, dirty, err := m.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			fmt.Fprintf(os.Stderr, "migrate version: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("version=%d dirty=%v\n", version, dirty)

	case "force":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: migrate force <version>")
			os.Exit(1)
		}
		v, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid version: %v\n", err)
			os.Exit(1)
		}
		if err := m.Force(v); err != nil {
			fmt.Fprintf(os.Stderr, "migrate force: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("forced to version %d\n", v)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: migrate <command>")
	fmt.Println("Commands:")
	fmt.Println("  up               Apply all pending migrations")
	fmt.Println("  down             Roll back the last migration")
	fmt.Println("  version          Print current schema version")
	fmt.Println("  force <version>  Force schema version (use after dirty state)")
}
