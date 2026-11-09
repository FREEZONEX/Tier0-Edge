package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type VideoCamera struct {
	ID          int64  `gorm:"column:id;primaryKey" json:"id"`
	CameraCode  string `gorm:"column:camera_code;size:128;not null" json:"cameraCode"`
	Name        string `gorm:"column:name;size:128;not null" json:"name"`
	Location    string `gorm:"column:location;size:256;not null;default:''" json:"location"`
	Description string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	Status      int    `gorm:"column:status;not null;default:1" json:"status"`
	SoftNoDelByTime
}

func (VideoCamera) TableName() string { return "sys_video_camera" }

type VideoChannel struct {
	ID                   int64      `gorm:"column:id;primaryKey" json:"id"`
	CameraID             int64      `gorm:"column:camera_id;not null" json:"cameraId"`
	ChannelCode          string     `gorm:"column:channel_code;size:128;not null" json:"channelCode"`
	Transport            string     `gorm:"column:transport;size:32;not null" json:"transport"`
	SourceURL            string     `gorm:"column:source_url;type:text;not null;default:''" json:"sourceUrl"`
	CredentialCiphertext string     `gorm:"column:credential_ciphertext;type:text;not null;default:''" json:"-"`
	RTPType              string     `gorm:"column:rtp_type;size:16;not null;default:'tcp'" json:"rtpType"`
	App                  string     `gorm:"column:app;size:64;not null;default:'live'" json:"app"`
	Stream               string     `gorm:"column:stream;size:128;not null" json:"stream"`
	LiveStatus           string     `gorm:"column:live_status;size:32;not null;default:'unknown'" json:"liveStatus"`
	Resolution           string     `gorm:"column:resolution;size:32;not null;default:''" json:"resolution"`
	LastError            string     `gorm:"column:last_error;type:text;not null;default:''" json:"lastError"`
	LastTestedAt         *time.Time `gorm:"column:last_tested_at" json:"lastTestedAt"`
	SoftNoDelByTime
}

func (VideoChannel) TableName() string { return "sys_video_channel" }

type VideoCameraFilter struct {
	Page      int
	Size      int
	Keyword   string
	Status    int
	Transport string
}

type VideoRepo struct{ db *gorm.DB }

func NewVideoRepo(in any) *VideoRepo { return &VideoRepo{db: GetCommonConn(in)} }

func (r *VideoRepo) CreateCamera(ctx context.Context, camera VideoCamera, channel VideoChannel) (VideoCamera, VideoChannel, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&camera).Error; err != nil {
			return normalizeDBError(err)
		}
		channel.CameraID = camera.ID
		if err := tx.Create(&channel).Error; err != nil {
			return normalizeDBError(err)
		}
		return nil
	})
	return camera, channel, normalizeDBError(err)
}

func (r *VideoRepo) GetCamera(ctx context.Context, id int64) (VideoCamera, error) {
	var item VideoCamera
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VideoRepo) GetPrimaryChannel(ctx context.Context, cameraID int64) (VideoChannel, error) {
	var item VideoChannel
	err := r.db.WithContext(ctx).
		Where("camera_id = ? AND deleted_time = 0", cameraID).
		Order("id").Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VideoRepo) ListCameras(ctx context.Context, filter VideoCameraFilter) ([]VideoCamera, int64, error) {
	query := r.db.WithContext(ctx).Model(&VideoCamera{}).Where("sys_video_camera.deleted_time = 0")
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("(sys_video_camera.name ILIKE ? OR sys_video_camera.camera_code ILIKE ? OR sys_video_camera.location ILIKE ?)", like, like, like)
	}
	if filter.Status != 0 {
		query = query.Where("sys_video_camera.status = ?", filter.Status)
	}
	if filter.Transport != "" {
		query = query.Joins("JOIN sys_video_channel ON sys_video_channel.camera_id = sys_video_camera.id AND sys_video_channel.deleted_time = 0").
			Where("sys_video_channel.transport = ?", filter.Transport)
	}
	var total int64
	if err := query.Distinct("sys_video_camera.id").Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]VideoCamera, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Select("sys_video_camera.*").Distinct().Order("sys_video_camera.created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *VideoRepo) ListChannelsByCameraIDs(ctx context.Context, cameraIDs []int64) ([]VideoChannel, error) {
	if len(cameraIDs) == 0 {
		return []VideoChannel{}, nil
	}
	items := make([]VideoChannel, 0)
	err := r.db.WithContext(ctx).
		Where("camera_id IN ? AND deleted_time = 0", cameraIDs).
		Order("camera_id, id").Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *VideoRepo) ListChannels(ctx context.Context, cameraID int64) ([]VideoChannel, error) {
	query := r.db.WithContext(ctx).Where("deleted_time = 0")
	if cameraID != 0 {
		query = query.Where("camera_id = ?", cameraID)
	}
	items := make([]VideoChannel, 0)
	err := query.Order("camera_id, id").Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *VideoRepo) UpdateCameraAndChannel(ctx context.Context, id int64, cameraUpdates, channelUpdates map[string]any) error {
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cameraResult := tx.Model(&VideoCamera{}).
			Where("id = ? AND deleted_time = 0", id).
			Updates(touchByValues(cameraUpdates, valueAsInt64(cameraUpdates["updated_by"]), 0))
		if cameraResult.Error != nil {
			return normalizeDBError(cameraResult.Error)
		}
		if cameraResult.RowsAffected == 0 {
			return ErrNotFound
		}
		if len(channelUpdates) == 0 {
			return nil
		}
		channelResult := tx.Model(&VideoChannel{}).
			Where("camera_id = ? AND deleted_time = 0", id).
			Updates(touchByValues(channelUpdates, valueAsInt64(channelUpdates["updated_by"]), 0))
		if channelResult.Error != nil {
			return normalizeDBError(channelResult.Error)
		}
		if channelResult.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}))
}

// UpdateChannelRuntime 更新主通道的连接探测结果（连接状态、分辨率、最近错误和探测时间）。
func (r *VideoRepo) UpdateChannelRuntime(ctx context.Context, cameraID int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&VideoChannel{}).
		Where("camera_id = ? AND deleted_time = 0", cameraID).
		Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *VideoRepo) SoftDeleteCamera(ctx context.Context, id, userID int64) error {
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := repoNowMilli()
		cameraResult := tx.Model(&VideoCamera{}).
			Where("id = ? AND deleted_time = 0", id).
			Updates(softDeleteNoDelByValues(userID, now))
		if cameraResult.Error != nil {
			return normalizeDBError(cameraResult.Error)
		}
		if cameraResult.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Model(&VideoChannel{}).
			Where("camera_id = ? AND deleted_time = 0", id).
			Updates(softDeleteNoDelByValues(userID, now)).Error; err != nil {
			return normalizeDBError(err)
		}
		return nil
	}))
}

func valueAsInt64(value any) int64 {
	if out, ok := value.(int64); ok {
		return out
	}
	return 0
}
