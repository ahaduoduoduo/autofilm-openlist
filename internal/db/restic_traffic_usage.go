package db

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
)

func AddResticTrafficUsage(repository, day string, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	usage := model.ResticTrafficUsage{Repository: repository, Day: day}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.FirstOrCreate(&usage, model.ResticTrafficUsage{
			Repository: repository,
			Day:        day,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&usage).UpdateColumn("bytes", gorm.Expr("bytes + ?", bytes)).Error
	})
}

func SumResticTrafficUsage(repository, dayPrefix string) (int64, error) {
	var total int64
	query := db.Model(&model.ResticTrafficUsage{}).
		Where("day LIKE ?", dayPrefix+"%")
	if repository != "" {
		query = query.Where("repository = ?", repository)
	}
	err := query.Select("COALESCE(SUM(bytes), 0)").Scan(&total).Error
	return total, err
}
