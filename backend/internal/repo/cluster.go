package repo

import "gorm.io/gorm"

// ClusterRepo stores the existing one-way user synchronization state.
type ClusterRepo struct{ db *gorm.DB }

func NewClusterRepo(in any) *ClusterRepo { return &ClusterRepo{db: GetCommonConn(in)} }
