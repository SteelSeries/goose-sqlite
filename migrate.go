package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type MigrationRecord struct {
	VersionId int64
	TStamp    time.Time
	IsApplied int // was this a result of up() (1) or down() (0)
}

type Migration struct {
	Version  int64
	Next     int64  // next version, or -1 if none
	Previous int64  // previous version, -1 if none
	Source   string // .go or .sql script
}

type MigrationSlice []Migration

// helpers so we can use pkg sort
func (s MigrationSlice) Len() int           { return len(s) }
func (s MigrationSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s MigrationSlice) Less(i, j int) bool { return s[i].Version < s[j].Version }

type MigrationMap struct {
	Migrations MigrationSlice // migrations, sorted according to Direction
	Direction  int            // sort direction: 1 -> Up, 0 -> Down
}

func runMigrations(conf *DBConf, migrationsDir string, target int64, allowOutOfOrder bool) {

	db, err := sql.Open(conf.Driver, conf.OpenStr)
	if err != nil {
		log.Fatal("couldn't open DB:", err)
	}
	defer db.Close()

	current, e := ensureDBVersion(db)
	if e != nil {
		log.Fatalf("couldn't get DB version: %v", e)
	}

	var mm *MigrationMap
	if allowOutOfOrder {
		mm, err = collectAllUnappliedMigrations(migrationsDir, current, target, db)
	} else {
		mm, err = collectMigrations(migrationsDir, current, target)
	}

	if err != nil {
		log.Fatal(err)
	}

	if len(mm.Migrations) == 0 {
		fmt.Printf("goose: no migrations to run. current version: %d\n", current)
		return
	}

	if allowOutOfOrder {
		mm.Sort(true)
	} else {
		mm.Sort(current < target)
	}

	fmt.Printf("goose: migrating db environment '%v', current version: %d, target: %d\n",
		conf.Env, current, target)

	fmt.Println("About to loop through migrations")

	for idx, m := range mm.Migrations {
		fmt.Println("Iterating through migrations: ", idx)
		var e error

		switch filepath.Ext(m.Source) {
		case ".go":
			fmt.Println("Running go migration")
			e = runGoMigration(conf, m.Source, m.Version, mm.Direction)
		case ".sql":
			fmt.Println("Running sql migration")
			e = runSQLMigration(db, m.Source, m.Version, mm.Direction)
		}

		fmt.Println("Before migration error check")
		if e != nil {
			fmt.Println("Hitting fatal error")
			log.Fatalf("FAIL %v, quitting migration", e)
		}

		fmt.Println("OK   ", filepath.Base(m.Source))
	}
}

// collect all the valid looking migration scripts in the
// migrations folder, and key them by version
func collectMigrations(dirpath string, current, target int64) (mm *MigrationMap, err error) {

	mm = &MigrationMap{}

	// extract the numeric component of each migration,
	// filter out any uninteresting files,
	// and ensure we only have one file per migration version.
	filepath.Walk(dirpath, func(name string, info os.FileInfo, err error) error {

		if v, e := numericComponent(name); e == nil {

			for _, m := range mm.Migrations {
				if v == m.Version {
					log.Fatalf("more than one file specifies the migration for version %d (%s and %s)",
						v, m.Source, filepath.Join(dirpath, name))
				}
			}

			if versionFilter(v, current, target) {
				mm.Append(v, name)
			}
		}

		return nil
	})

	return mm, nil
}

// collect all migrations that have not been run, including ones older than the current version
// This is only valid for migrating up
func collectAllUnappliedMigrations(dirpath string, current, target int64, db *sql.DB) (mm *MigrationMap, err error) {
	mm = &MigrationMap{}

	// extract the numeric component of each migration,
	// filter out any uninteresting files,
	// and ensure we only have one file per migration version.
	filepath.Walk(dirpath, func(name string, info os.FileInfo, err error) error {

		if v, e := numericComponent(name); e == nil {

			for _, m := range mm.Migrations {
				if v == m.Version {
					log.Fatalf("more than one file specifies the migration for version %d (%s and %s)",
						v, m.Source, filepath.Join(dirpath, name))
				}
			}

			row := db.QueryRow("SELECT version_id, is_applied from goose_db_version where version_id=$1;", v)

			var record MigrationRecord
			err := row.Scan(&record.VersionId, &record.IsApplied)
			if (err == sql.ErrNoRows) || (record.IsApplied == 0) {
				mm.Append(v, name)
			}
		}

		return nil
	})

	return mm, nil
}

func versionFilter(v, current, target int64) bool {

	if target > current {
		return v > current && v <= target
	}

	if target < current {
		return v <= current && v > target
	}

	return false
}

func (mm *MigrationMap) Append(v int64, source string) {
	mm.Migrations = append(mm.Migrations, Migration{
		Version:  v,
		Next:     -1,
		Previous: -1,
		Source:   source,
	})
}

func (mm *MigrationMap) Sort(direction bool) {
	sort.Sort(mm.Migrations)

	// set direction, and reverse order if need be
	if direction {
		mm.Direction = 1
	} else {
		mm.Direction = 0
	}

	if mm.Direction == 0 {
		for i, j := 0, len(mm.Migrations)-1; i < j; i, j = i+1, j-1 {
			mm.Migrations[i], mm.Migrations[j] = mm.Migrations[j], mm.Migrations[i]
		}
	}

	// now that we're sorted in the appropriate direction,
	// populate next and previous for each migration
	for i, m := range mm.Migrations {
		prev := int64(-1)
		if i > 0 {
			prev = mm.Migrations[i-1].Version
			mm.Migrations[i-1].Next = m.Version
		}
		mm.Migrations[i].Previous = prev
	}
}

// look for migration scripts with names in the form:
//  XXX_descriptivename.ext
// where XXX specifies the version number
// and ext specifies the type of migration
func numericComponent(name string) (int64, error) {

	base := filepath.Base(name)

	if ext := filepath.Ext(base); ext != ".go" && ext != ".sql" {
		return 0, errors.New("not a recognized migration file type")
	}

	idx := strings.Index(base, "_")
	if idx < 0 {
		return 0, errors.New("no separator found")
	}

	n, e := strconv.ParseInt(base[:idx], 10, 64)
	if e == nil && n <= 0 {
		return 0, errors.New("migration IDs must be greater than zero")
	}

	return n, e
}

// retrieve the current version for this DB.
// Create and initialize the DB version table if it doesn't exist.
func ensureDBVersion(db *sql.DB) (int64, error) {

	rows, err := db.Query("SELECT version_id, is_applied from goose_db_version ORDER BY id DESC;")
	if err != nil {
		// XXX: cross platform method to detect failure reason
		// for now, assume it was because the table didn't exist, and try to create it
		return 0, createVersionTable(db)
	}

	defer rows.Close()

	// The most recent record for each migration specifies
	// whether it has been applied or rolled back.
	// The first version we find that has been applied is the current version.

	toSkip := make([]int64, 0)

	for rows.Next() {
		var row MigrationRecord
		if err = rows.Scan(&row.VersionId, &row.IsApplied); err != nil {
			log.Fatal("error scanning rows:", err)
		}

		// have we already marked this version to be skipped?
		skip := false
		for _, v := range toSkip {
			if v == row.VersionId {
				skip = true
				break
			}
		}

		// if version has been applied and not marked to be skipped, we're done
		if row.IsApplied == 1 && !skip {
			return row.VersionId, nil
		}

		// version is either not applied, or we've already seen a more
		// recent version of it that was not applied.
		if !skip {
			toSkip = append(toSkip, row.VersionId)
		}
	}

	panic("failure in ensureDBVersion()")
}

func createVersionTable(db *sql.DB) error {
	txn, err := db.Begin()
	if err != nil {
		return err
	}

	// create the table and insert an initial value of 0
	create := `CREATE TABLE goose_db_version (
               id INTEGER PRIMARY KEY  AUTOINCREMENT  UNIQUE,
               version_id integer NOT NULL,
               is_applied integer NOT NULL,
               tstamp text NULL default (datetime('now'))
               );`
	insert := "INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1);"

	for _, str := range []string{create, insert} {
		if _, err := txn.Exec(str); err != nil {
			txn.Rollback()
			return err
		}
	}

	return txn.Commit()
}

// wrapper for ensureDBVersion for callers that don't already have
// their own DB instance
func getDBVersion(conf *DBConf) int64 {

	db, err := sql.Open(conf.Driver, conf.OpenStr)
	if err != nil {
		log.Fatal("couldn't open DB:", err)
	}
	defer db.Close()

	version, err := ensureDBVersion(db)
	if err != nil {
		log.Fatalf("couldn't get DB version: %v", err)
	}

	return version
}
