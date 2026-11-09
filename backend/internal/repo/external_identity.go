package repo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ExternalIdentity struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	UserID    int64     `gorm:"column:user_id"`
	Provider  string    `gorm:"column:provider"`
	Issuer    string    `gorm:"column:issuer"`
	TenantID  string    `gorm:"column:tenant_id"`
	SubjectID string    `gorm:"column:subject_id"`
	CreatedAt time.Time `gorm:"column:created_time"`
	UpdatedAt time.Time `gorm:"column:updated_time"`
}

func (ExternalIdentity) TableName() string { return "sys_user_identity" }

func (r *IAMRepo) FindExternalIdentity(ctx context.Context, provider, issuer, tenantID, subjectID string) (ExternalIdentity, error) {
	var identity ExternalIdentity
	err := r.db.WithContext(ctx).
		Where("provider = ? AND issuer = ? AND tenant_id = ? AND subject_id = ?",
			strings.TrimSpace(provider), strings.TrimSpace(issuer), strings.TrimSpace(tenantID), strings.TrimSpace(subjectID)).
		Take(&identity).Error
	return identity, err
}

func (r *IAMRepo) UsernameAvailable(ctx context.Context, username string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&sysUserInfo{}).
		Where("LOWER(user_name) = LOWER(?) AND deleted_time = 0", username).
		Count(&count).Error
	return count == 0, err
}

func (r *IAMRepo) LinkExternalIdentity(ctx context.Context, userID int64, identity ExternalIdentity) error {
	if userID <= 0 || !validExternalIdentity(identity) {
		return ErrInvalidArgument
	}
	identity.UserID = userID
	identity.ID = 0
	identity.CreatedAt = time.Now().UTC()
	identity.UpdatedAt = identity.CreatedAt
	return normalizeDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user sysUserInfo
		if err := tx.Where("user_id = ? AND deleted_time = 0", userID).Take(&user).Error; err != nil {
			return err
		}
		return tx.Create(&identity).Error
	}))
}

func (r *IAMRepo) CreateExternalUser(ctx context.Context, input UserCreate, identity ExternalIdentity) (User, error) {
	if !validExternalIdentity(identity) {
		return User{}, ErrInvalidArgument
	}
	var created User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, _, err := createUserInTx(tx, input)
		if err != nil {
			return err
		}
		identity.ID = 0
		identity.UserID = user.ID
		identity.CreatedAt = time.Now().UTC()
		identity.UpdatedAt = identity.CreatedAt
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		created = user
		return nil
	})
	if err != nil {
		return User{}, normalizeDBError(err)
	}
	return created, nil
}

func validExternalIdentity(identity ExternalIdentity) bool {
	return strings.TrimSpace(identity.Provider) != "" &&
		strings.TrimSpace(identity.Issuer) != "" &&
		strings.TrimSpace(identity.TenantID) != "" &&
		strings.TrimSpace(identity.SubjectID) != ""
}
