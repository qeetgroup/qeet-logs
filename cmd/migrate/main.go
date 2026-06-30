// Command migrate applies golang-migrate SQL migrations to the Postgres
// metadata database. ClickHouse DDL is applied separately via `make ch-migrate`.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	url := flag.String("url", "", "PostgreSQL connection URL (required)")
	dir := flag.String("dir", "migrations", "Migrations directory")
	flag.Parse()

	if *url == "" {
		*url = os.Getenv("DATABASE_URL")
	}
	if *url == "" {
		log.Fatal("-url or DATABASE_URL required")
	}

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("command required: up | down [n] | down -all | force <v> | version | drop")
	}

	m, err := migrate.New("file://"+*dir, *url)
	if err != nil {
		log.Fatalf("migrate.New: %v", err)
	}
	defer m.Close()

	cmd := args[0]
	switch cmd {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate up: %v", err)
		}
		fmt.Println("migrations applied")

	case "down":
		if len(args) > 1 && args[1] == "-all" {
			if err := m.Down(); err != nil && err != migrate.ErrNoChange {
				log.Fatalf("migrate down all: %v", err)
			}
			fmt.Println("all migrations rolled back")
			return
		}
		n := 1
		if len(args) > 1 {
			n, _ = strconv.Atoi(args[1])
		}
		if err := m.Steps(-n); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate down: %v", err)
		}
		fmt.Printf("rolled back %d migration(s)\n", n)

	case "force":
		if len(args) < 2 {
			log.Fatal("force requires version number")
		}
		v, _ := strconv.Atoi(args[1])
		if err := m.Force(v); err != nil {
			log.Fatalf("migrate force: %v", err)
		}
		fmt.Printf("forced version %d\n", v)

	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("version: %v", err)
		}
		fmt.Printf("version: %d, dirty: %v\n", v, dirty)

	case "drop":
		if err := m.Drop(); err != nil {
			log.Fatalf("drop: %v", err)
		}
		fmt.Println("all tables dropped")

	default:
		log.Fatalf("unknown command: %s", cmd)
	}
}
