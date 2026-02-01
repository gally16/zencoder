package database

import (
	"fmt"
	"strings"

	"zencoder2api/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Init 初始化数据库连接
// dbType: sqlite, postgres, mysql
// dsn: 数据库连接字符串
func Init(dbType, dsn string) error {
	var dialector gorm.Dialector

	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		dialector = postgres.Open(dsn)
	case "mysql":
		dialector = mysql.Open(dsn)
	case "sqlite", "":
		dialector = sqlite.Open(dsn)
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	var err error
	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
	}

	return DB.AutoMigrate(
		&model.Account{},
		&model.TokenRecord{},
		&model.GenerationTask{},
	)
}

func GetDB() *gorm.DB {
	return DB
}
