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
	Help:    `Run with the additional "outOfOrder" arg to also run older migrations that were previously missed`,
}

func upRun(cmd *Command, args ...string) {

	allowOutOfOrder := args[0] == "outOfOrder"
	conf, err := MakeDBConf()
	if err != nil {
		log.Fatal(err)
	}

	target := mostRecentVersionAvailable(conf.MigrationsDir)
	runMigrations(conf, conf.MigrationsDir, target, allowOutOfOrder)
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
