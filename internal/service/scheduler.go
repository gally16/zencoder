package service

import (
	"log"
	"time"

	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
)

func StartCreditResetScheduler() {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day(), 9, 9, 0, 0, now.Location())
			if now.After(next) {
				next = next.Add(24 * time.Hour)
			}

			time.Sleep(time.Until(next))
			ResetAllCredits()
		}
	}()
	log.Println("Credit reset scheduler started (daily at 09:09)")
}

func ResetAllCredits() {
	today := time.Now().Format("2006-01-02")

	database.GetDB().Model(&model.Account{}).
		Where("last_reset_date != ? OR last_reset_date IS NULL", today).
		Updates(map[string]interface{}{
			"daily_used":      0,
			"is_cooling":      false,
			"last_reset_date": today,
		})

	log.Printf("Credits reset completed at %s", time.Now().Format("2006-01-02 15:04:05"))
}
