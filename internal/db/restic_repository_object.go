package db

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResticRepositoryStorage struct {
	StoredBytes  int64
	ObjectCount  int64
	Initialized  bool
	LastVerified time.Time
}

func UpsertResticRepositoryObject(repository, objectType, name string, size int64) error {
	object := model.ResticRepositoryObject{
		Repository: repository,
		ObjectType: objectType,
		Name:       name,
		Size:       size,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "repository"},
			{Name: "object_type"},
			{Name: "name"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"size", "updated_at"}),
	}).Create(&object).Error
}

func DeleteResticRepositoryObject(repository, objectType, name string) error {
	return db.Where(
		"repository = ? AND object_type = ? AND name = ?",
		repository,
		objectType,
		name,
	).Delete(&model.ResticRepositoryObject{}).Error
}

func ReplaceResticRepositoryObjects(repository string, objects []model.ResticRepositoryObject) error {
	now := time.Now()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("repository = ?", repository).
			Delete(&model.ResticRepositoryObject{}).Error; err != nil {
			return err
		}
		for index := range objects {
			objects[index].Repository = repository
		}
		if len(objects) > 0 {
			if err := tx.CreateInBatches(objects, 500).Error; err != nil {
				return err
			}
		}
		inventory := model.ResticRepositoryInventory{
			Repository:  repository,
			Initialized: true,
			ObjectCount: int64(len(objects)),
			RefreshedAt: now,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"initialized",
				"object_count",
				"refreshed_at",
				"updated_at",
			}),
		}).Create(&inventory).Error
	})
}

func ResticRepositoryStorageUsage(repository string) (ResticRepositoryStorage, error) {
	var inventory model.ResticRepositoryInventory
	result := ResticRepositoryStorage{}
	err := db.First(&inventory, "repository = ?", repository).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return result, err
	}
	if err == nil {
		result.Initialized = inventory.Initialized
		result.LastVerified = inventory.RefreshedAt
	}
	row := db.Model(&model.ResticRepositoryObject{}).
		Where("repository = ?", repository).
		Select("COALESCE(SUM(size), 0) AS stored_bytes, COUNT(*) AS object_count").
		Row()
	if err := row.Scan(&result.StoredBytes, &result.ObjectCount); err != nil {
		return ResticRepositoryStorage{}, err
	}
	return result, nil
}
