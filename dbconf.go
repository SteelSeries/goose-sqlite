package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/erh/go-gypsy/yaml"
	_ "github.com/mattn/go-sqlite3"
)

// global options. available to any subcommands.
var dbConfPath = flag.String("path", "db", "folder containing db info")
var dbEnv = flag.String("env", "development", "which DB environment to use")

// Manual goose-sqlite command without configuration file
var dbPath = flag.String("dbPath", "", "path to db file")
var dbMigrationPath = flag.String("dbMigration", "", "path to db migrations")
var dbDriver = flag.String("dbDriver", "", "the sql driver to use")

type DBConf struct {
	MigrationsDir string
	Env           string
	Driver        string
	OpenStr       string
}

// extract configuration details from the given file
func MakeDBConf() (*DBConf, error) {
	// Check to see if dbPath, dbMigrationPath, dbDriver were provided
	if *dbPath != "" && *dbMigrationPath != "" && *dbDriver != "" {
		return &DBConf{
			MigrationsDir: *dbMigrationPath,
			Env:           "",
			Driver:        *dbDriver,
			OpenStr:       *dbPath,
		}, nil
	}

	cfgFile := filepath.Join(*dbConfPath, "dbconf.yml")

	f, err := yaml.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}

	drv, derr := f.Get(fmt.Sprintf("%s.driver", *dbEnv))
	if derr != nil {
		return nil, derr
	}

	open, oerr := f.Get(fmt.Sprintf("%s.open", *dbEnv))
	if oerr != nil {
		return nil, oerr
	}
	open = os.ExpandEnv(open)

	// Automatically parse postgres urls

	return &DBConf{
		MigrationsDir: filepath.Join(*dbConfPath, "migrations"),
		Env:           *dbEnv,
		Driver:        drv,
		OpenStr:       open,
	}, nil
}
