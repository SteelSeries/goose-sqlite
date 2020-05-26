package main

import (
	"log"
	"os"
	"path/filepath"
)

var upCmd = &Command{
	Name:    "up",
	Usage:   "",
	Summary: "Migrate the DB to the most recent version available",
	Help:    `Run with the "--outOfOrder" flag to also run older migrations that were previously missed`,
}
var outOfOrder = upCmd.Flag.Bool("outOfOrder", false, "Allow previously missed migrations to be run out of order")

func upRun(cmd *Command, args ...string) {
	conf, err := MakeDBConf()
	if err != nil {
		log.Fatal(err)
	}

	target := mostRecentVersionAvailable(conf.MigrationsDir)
	runMigrations(conf, conf.MigrationsDir, target, *outOfOrder)
}

// helper to identify the most recent possible version
// within a folder of migration scripts
func mostRecentVersionAvailable(dirpath string) int64 {

	mostRecent := int64(-1)

	filepath.Walk(dirpath, func(name string, info os.FileInfo, err error) error {

		if v, e := numericComponent(name); e == nil {
			if v > mostRecent {
				mostRecent = v
			}
		}

		return nil
	})

	return mostRecent
}

func init() {
	upCmd.Run = upRun
}
