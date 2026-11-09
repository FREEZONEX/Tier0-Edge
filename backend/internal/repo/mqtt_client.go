package repo

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ConnectClientTypeDevice = 1
	ConnectClientTypeFlow   = 2
	ConnectClientTypeEdge   = 3
	ConnectClientTypeSystem = 4

	ConnectStatusOnline  = 1
	ConnectStatusOffline = 2
	ConnectStatusActive  = ConnectStatusOnline
	AuthStatusActive     = 1
	AuthStatusDisabled   = 2
)

type ConnectClient struct {
	ID                          int64  `gorm:"column:id;primaryKey;autoIncrement:false" json:"id"`
	Name                        string `gorm:"column:name;size:128" json:"name"`
	ConnectStatus               int    `gorm:"column:connect_status;default:2" json:"connectStatus"`
	AuthStatus                  int    `gorm:"column:auth_status;default:1" json:"authStatus"`
	Description                 string `gorm:"column:description;type:text" json:"description"`
	Password                    string `gorm:"column:password;size:255" json:"password,omitempty"`
	ClientID                    string `gorm:"column:client_id;size:64;not null" json:"clientId"`
	Username                    string `gorm:"column:username;size:64;not null" json:"username"`
	Type                        int    `gorm:"column:type" json:"type"`
	IP                          string `gorm:"column:ip;size:64" json:"ip"`
	ConnectedTime               int64  `gorm:"column:connected_time;default:0" json:"connectedTime"`
	ClientIDRandomSuffixEnabled bool   `gorm:"column:client_id_random_suffix_enabled;not null;default:false" json:"clientIdRandomSuffixEnabled"`
	SoftTime
}

func (ConnectClient) TableName() string { return "uns_connect_client" }

type ConnectClientFilter struct {
	Type    int
	Keyword string
}

func (r *ConnectClientRepo) CreateConnectClient(ctx context.Context, item ConnectClient) (ConnectClient, error) {
	now := time.Now().UTC().UnixMilli()
	ensureID(&item.ID)
	if item.ConnectStatus == 0 {
		item.ConnectStatus = ConnectStatusOffline
	}
	if item.AuthStatus == 0 {
		item.AuthStatus = AuthStatusActive
	}
	if item.Type == 0 {
		item.Type = ConnectClientTypeDevice
	}
	if item.Password == "" {
		item.Password = randomHex(15)
	}
	clientID, username := buildConnectCredential(item.ID)
	if item.Type == ConnectClientTypeEdge {
		username = clientID + "&edgeNode"
	}
	row := ConnectClient{
		ID:                          item.ID,
		Name:                        strings.TrimSpace(item.Name),
		ConnectStatus:               item.ConnectStatus,
		AuthStatus:                  item.AuthStatus,
		Description:                 strings.TrimSpace(item.Description),
		Password:                    item.Password,
		ClientID:                    clientID,
		Username:                    username,
		Type:                        item.Type,
		ConnectedTime:               0,
		ClientIDRandomSuffixEnabled: connectClientTypeSupportsRandomSuffix(item.Type) && item.ClientIDRandomSuffixEnabled,
	}
	row.CreatedBy = item.CreatedBy
	row.UpdatedBy = item.CreatedBy
	ts := repoTimeFromMilli(now)
	row.CreatedTime = ts
	row.UpdatedTime = ts
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return ConnectClient{}, normalizeDBError(err)
	}
	return row, nil
}

func (r *ConnectClientRepo) ListConnectClients(ctx context.Context, filter ConnectClientFilter) ([]ConnectClient, error) {
	q := r.db.WithContext(ctx).Where("deleted_time = 0")
	if filter.Type != 0 {
		q = q.Where("type = ?", filter.Type)
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("(name ILIKE ? OR description ILIKE ? OR client_id ILIKE ? OR username ILIKE ?)", like, like, like, like)
	}
	var out []ConnectClient
	err := q.Order("id DESC").Find(&out).Error
	return out, err
}

func (r *ConnectClientRepo) GetConnectClient(ctx context.Context, id int64) (ConnectClient, error) {
	var item ConnectClient
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, err
}

func (r *ConnectClientRepo) ResetConnectClientPassword(ctx context.Context, id, userID int64) (ConnectClient, error) {
	var item ConnectClient
	res := r.db.WithContext(ctx).Model(&item).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{
			"password":       randomHex(15),
			"connect_status": ConnectStatusOffline,
		}, userID, 0))
	if res.Error != nil {
		return ConnectClient{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ConnectClient{}, ErrNotFound
	}
	return item, nil
}

func (r *ConnectClientRepo) SetConnectClientAuthStatus(ctx context.Context, id, userID int64, status int) (ConnectClient, error) {
	var item ConnectClient
	updates := touchByValues(map[string]any{"auth_status": status}, userID, 0)
	if status == AuthStatusDisabled {
		updates["connect_status"] = ConnectStatusOffline
	}
	res := r.db.WithContext(ctx).Model(&item).Clauses(returningAll()).
		Where("id = ? AND deleted_time = 0", id).
		Updates(updates)
	if res.Error != nil {
		return ConnectClient{}, res.Error
	}
	if res.RowsAffected == 0 {
		return ConnectClient{}, ErrNotFound
	}
	return item, nil
}

func (r *ConnectClientRepo) DeleteConnectClient(ctx context.Context, id, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ConnectClient{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(softDeleteValues(userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ConnectClientRepo) DeleteConnectClientByType(ctx context.Context, id, userID int64, clientType int) (ConnectClient, bool, error) {
	var item ConnectClient
	res := r.db.WithContext(ctx).Model(&item).Clauses(returningAll()).
		Where("id = ? AND type = ? AND deleted_time = 0", id, clientType).
		Updates(softDeleteValues(userID, 0))
	if res.Error != nil {
		return ConnectClient{}, false, res.Error
	}
	return item, res.RowsAffected > 0, nil
}

// EnableConnectClientRandomSuffix 允许该客户端以 clientId 随机后缀（base_x / base-x）连接，
// 用于多个浏览器页面并发预览（同 clientId 会被 broker 互踢）。
func (r *ConnectClientRepo) EnableConnectClientRandomSuffix(ctx context.Context, id, userID int64) error {
	res := r.db.WithContext(ctx).Model(&ConnectClient{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(touchByValues(map[string]any{"client_id_random_suffix_enabled": true}, userID, 0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ConnectClientRepo) ValidateMqttClient(ctx context.Context, username, password, clientID string) (bool, error) {
	_, ok, err := r.FindAuthorizedMqttClient(ctx, username, password, clientID)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (r *ConnectClientRepo) FindAuthorizedMqttClient(ctx context.Context, username, password, clientID string) (ConnectClient, bool, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	clientID = strings.TrimSpace(clientID)
	if username == "" || password == "" {
		return ConnectClient{}, false, nil
	}
	usernames := connectClientUsernameCandidates(username)
	var rows []ConnectClient
	err := r.db.WithContext(ctx).Model(&ConnectClient{}).
		Where("username IN ? AND password = ? AND auth_status = 1 AND deleted_time = 0", usernames, password).
		Order("id").
		Find(&rows).Error
	if err != nil {
		return ConnectClient{}, false, err
	}
	for _, row := range rows {
		if connectClientMatchesInputClientID(row, username, clientID) {
			return row, true, nil
		}
	}
	return ConnectClient{}, false, nil
}

func (r *ConnectClientRepo) FindMqttClientByIdentity(ctx context.Context, username, clientID string) (ConnectClient, bool, error) {
	username = strings.TrimSpace(username)
	clientID = strings.TrimSpace(clientID)
	if username == "" && clientID == "" {
		return ConnectClient{}, false, nil
	}
	usernames := connectClientUsernameCandidates(username)
	clientIDs := connectClientIDCandidates(username, clientID)
	q := r.db.WithContext(ctx).Model(&ConnectClient{}).Where("auth_status = 1 AND deleted_time = 0")
	if len(usernames) > 0 {
		var rows []ConnectClient
		err := q.Where("username IN ?", usernames).Order("id").Find(&rows).Error
		if err != nil {
			return ConnectClient{}, false, err
		}
		for _, row := range rows {
			if connectClientMatchesInputClientID(row, username, clientID) {
				return row, true, nil
			}
		}
		return ConnectClient{}, false, nil
	}
	if len(clientIDs) > 0 {
		var rows []ConnectClient
		err := q.Where("client_id IN ?", clientIDs).Order("id").Find(&rows).Error
		if err != nil {
			return ConnectClient{}, false, err
		}
		if len(rows) > 0 {
			return rows[0], true, nil
		}
	}
	return ConnectClient{}, false, nil
}

func (r *ConnectClientRepo) MarkConnectClientConnected(ctx context.Context, username, clientID, ip string) error {
	username = strings.TrimSpace(username)
	clientID = strings.TrimSpace(clientID)
	client, ok, err := r.FindMqttClientByIdentity(ctx, username, clientID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	updates := map[string]any{
		"connect_status": ConnectStatusOnline,
		"connected_time": now,
		"updated_time":   repoTimeFromMilli(now),
	}
	if value := strings.TrimSpace(ip); value != "" {
		updates["ip"] = value
	}
	return r.db.WithContext(ctx).Model(&ConnectClient{}).
		Where("id = ? AND deleted_time = 0", client.ID).
		Updates(updates).Error
}

func (r *ConnectClientRepo) CountConnectedExternalClients(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ConnectClient{}).
		Where("deleted_time = 0 AND connect_status = ? AND type = ?", ConnectStatusOnline, ConnectClientTypeDevice).
		Count(&count).Error
	return count, err
}

func connectClientUsernameCandidates(username string) []string {
	username = strings.TrimSpace(username)
	if username == "" {
		return []string{}
	}
	out := []string{username}
	if base, ok := strings.CutSuffix(username, "&edgeNode"); ok {
		base = strings.TrimSpace(base)
		if base != "" && base != username {
			out = append(out, base)
		}
	}
	return out
}

func connectClientIDCandidates(username, clientID string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 2)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(clientID)
	if base, ok := strings.CutSuffix(strings.TrimSpace(username), "&edgeNode"); ok {
		add(base)
	}
	return out
}

const maxConnectClientRandomSuffixLength = 32

var connectClientRandomSuffixSeparators = []string{"_", "-"}

func connectClientMatchesInputClientID(row ConnectClient, username, clientID string) bool {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return true
	}
	for _, candidate := range connectClientIDCandidates(username, clientID) {
		if candidate == row.ClientID {
			return true
		}
	}
	if row.Type == ConnectClientTypeEdge && clientID == strings.TrimSpace(row.ClientID)+"-fleet-agent" {
		return true
	}
	return connectClientMatchesRandomSuffix(row, clientID)
}

func connectClientMatchesRandomSuffix(row ConnectClient, clientID string) bool {
	if !row.ClientIDRandomSuffixEnabled || !connectClientTypeSupportsRandomSuffix(row.Type) {
		return false
	}
	base := strings.TrimSpace(row.ClientID)
	if base == "" {
		return false
	}
	for _, sep := range connectClientRandomSuffixSeparators {
		prefix := base + sep
		if strings.HasPrefix(clientID, prefix) {
			return validConnectClientRandomSuffix(strings.TrimPrefix(clientID, prefix))
		}
	}
	return false
}

func connectClientTypeSupportsRandomSuffix(clientType int) bool {
	return clientType == ConnectClientTypeDevice || clientType == ConnectClientTypeSystem
}

func validConnectClientRandomSuffix(suffix string) bool {
	if len(suffix) == 0 || len(suffix) > maxConnectClientRandomSuffixLength {
		return false
	}
	for _, ch := range suffix {
		if (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			continue
		}
		return false
	}
	return true
}

func buildConnectCredential(id int64) (string, string) {
	value := strconv.FormatInt(id, 10)
	return value, value
}

type ConnectClientRepo struct{ db *gorm.DB }

func NewConnectClientRepo(in any) *ConnectClientRepo {
	return &ConnectClientRepo{db: GetCommonConn(in)}
}
