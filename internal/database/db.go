package database

import (
	"zencoder2api/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init(dbPath string) error {
	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // 完全关闭日志输出
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
