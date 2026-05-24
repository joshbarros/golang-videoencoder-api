package database

import (
	"golang-videoencoder-api/domain"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Database stores runtime configuration and the active DB connection.
type Database struct {
	Db            *gorm.DB
	Dsn           string
	DsnTest       string
	DbType        string
	DbTypeTest    string
	Debug         bool
	AutoMigrateDb bool
	Env           string
}

// NewDb returns an empty Database configuration.
func NewDb() *Database {
	return &Database{}
}

// NewDbTest creates and connects an in-memory sqlite database for tests.
func NewDbTest() *gorm.DB {
	dbInstance := NewDb()
	dbInstance.Env = "test"
	dbInstance.DbTypeTest = "sqlite3"
	dbInstance.DsnTest = ":memory:"
	dbInstance.AutoMigrateDb = true
	dbInstance.Debug = true

	connection, err := dbInstance.Connect()

	if err != nil {
		log.Fatalf("Test database error: %v", err)
	}

	return connection
}

// Connect opens the configured database and runs optional migrations.
func (d *Database) Connect() (*gorm.DB, error) {
	var err error

	if d.Env != "test" {
		d.Db, err = gorm.Open(postgres.Open(d.Dsn), &gorm.Config{})
	} else {
		d.Db, err = gorm.Open(sqlite.Open(d.DsnTest), &gorm.Config{})
	}

	if err != nil {
		return nil, err
	}

	if d.Debug {
		d.Db = d.Db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Info)})
	}

	if d.AutoMigrateDb {
		err = d.Db.AutoMigrate(&domain.Video{}, &domain.Job{})
		if err != nil {
			return nil, err
		}
	}

	return d.Db, nil
}
