package db

import (
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResticRepositoryObjectInventory(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:restic-object-inventory?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	Init(database)

	require.NoError(t, ReplaceResticRepositoryObjects("synology", []model.ResticRepositoryObject{
		{ObjectType: "config", Name: "config", Size: 100},
		{ObjectType: "data", Name: "aa", Size: 200},
	}))
	require.NoError(t, UpsertResticRepositoryObject("synology", "data", "aa", 250))
	require.NoError(t, UpsertResticRepositoryObject("synology", "index", "bb", 50))
	require.NoError(t, DeleteResticRepositoryObject("synology", "config", "config"))

	storage, err := ResticRepositoryStorageUsage("synology")
	require.NoError(t, err)
	require.True(t, storage.Initialized)
	require.EqualValues(t, 300, storage.StoredBytes)
	require.EqualValues(t, 2, storage.ObjectCount)
	require.False(t, storage.LastVerified.IsZero())
}
