package db

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func AddResticTaskTrafficUsage(repository, task, day string, bytes, dailyLimitBytes int64, weight int) error {
	if task == "" {
		return nil
	}
	usage := model.ResticTaskTrafficUsage{
		Repository:      repository,
		Task:            task,
		Day:             day,
		DailyLimitBytes: dailyLimitBytes,
		Weight:          weight,
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository"}, {Name: "task"}, {Name: "day"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"daily_limit_bytes": dailyLimitBytes,
				"weight":            weight,
			}),
		}).Create(&usage).Error; err != nil {
			return err
		}
		if bytes <= 0 {
			return nil
		}
		return tx.Model(&model.ResticTaskTrafficUsage{}).
			Where("repository = ? AND task = ? AND day = ?", repository, task, day).
			UpdateColumn("bytes", gorm.Expr("bytes + ?", bytes)).Error
	})
}

func ListResticTaskTrafficUsage(day string) ([]model.ResticTaskTrafficUsage, error) {
	var result []model.ResticTaskTrafficUsage
	err := db.Where("day = ?", day).Find(&result).Error
	return result, err
}

func ReleaseResticTaskTrafficUsage(repository, task, day string, dailyLimitBytes int64, weight int, releasedAtBytes int64) error {
	usage := model.ResticTaskTrafficUsage{
		Repository:      repository,
		Task:            task,
		Day:             day,
		DailyLimitBytes: dailyLimitBytes,
		Weight:          weight,
		Released:        true,
		ReleasedAtBytes: releasedAtBytes,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "repository"}, {Name: "task"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"daily_limit_bytes": dailyLimitBytes,
			"weight":            weight,
			"released":          true,
			"released_at_bytes": releasedAtBytes,
		}),
	}).Create(&usage).Error
}
