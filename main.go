package main

import (
	"log"
	"necore/app"
	"necore/config"
	"necore/controller/router"
	"necore/database"
)

func main() {
	// This will print a hash of "test". U can insert it into sqlite3 manually for an admin account (the group section should be `["admin"]`).
	// dao.DebugTestPassword()

	if err := config.Init(); err != nil {
		log.Fatalf("initialize configuration: %v", err)
	}

	log.Printf("using configuration file: %s", config.Path())

	database.ConnectSqlite()
	router.SetupRoutes()
	app.Start()
}
