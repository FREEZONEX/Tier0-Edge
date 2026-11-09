package loginlog

import (
	"context"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"

	"github.com/mssola/useragent"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	EventLogin  = repo.UserLoginEventLogin
	EventLogout = repo.UserLoginEventLogout
)

type RecordInput struct {
	EventType string
	UserID    int64
	UserName  string
	IP        string
	UserAgent string
	Code      int64
	Msg       string
}

type PageQuery struct {
	PageNo    int
	PageSize  int
	Keyword   string
	UserName  string
	IpAddr    string
	EventType string
	Code      int
	StartTime string
	EndTime   string
}

type Service struct {
	repo *repo.UserLoginLogRepo
}

func New(ctx context.Context) *Service {
	return &Service{repo: repo.NewUserLoginLogRepo(ctx)}
}

// Record persists a login log entry; browser/os are parsed from UserAgent so
// callers never touch the raw header.
func (s *Service) Record(input RecordInput) {
	if s == nil {
		return
	}
	item := repo.UserLoginLog{
		UserID:    input.UserID,
		UserName:  strings.TrimSpace(input.UserName),
		IpAddr:    strings.TrimSpace(input.IP),
		EventType: eventOrLogin(input.EventType),
		Code:      input.Code,
		Msg:       input.Msg,
	}
	item.Browser, item.Os = parseUserAgent(input.UserAgent)
	// A bounded database write avoids losing login records in a process-local queue.
	if s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.repo.InsertUserLoginLog(ctx, item); err != nil {
		logx.Errorf("persist login log failed event=%s userID=%d: %v", item.EventType, item.UserID, err)
	}
}

func (s *Service) Page(ctx context.Context, query PageQuery) (map[string]any, error) {
	var code *int64
	if query.Code != 0 {
		value := int64(query.Code)
		code = &value
	}
	filter := repo.UserLoginLogFilter{
		PageNo:    query.PageNo,
		PageSize:  query.PageSize,
		Keyword:   query.Keyword,
		UserName:  query.UserName,
		IpAddr:    query.IpAddr,
		EventType: strings.TrimSpace(query.EventType),
		Code:      code,
		StartTime: parseTime(query.StartTime),
		EndTime:   parseTime(query.EndTime),
	}
	items, total, err := s.repo.ListUserLoginLogs(ctx, filter)
	if err != nil {
		return nil, err
	}
	pageNo := query.PageNo
	if pageNo <= 0 {
		pageNo = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return map[string]any{
		"pageNo":   pageNo,
		"pageSize": pageSize,
		"total":    total,
		"data":     items,
	}, nil
}

func eventOrLogin(value string) string {
	value = strings.TrimSpace(value)
	if value == repo.UserLoginEventLogout {
		return repo.UserLoginEventLogout
	}
	return repo.UserLoginEventLogin
}

func parseUserAgent(value string) (browser, os string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	parsed := useragent.New(value)
	name, _ := parsed.Browser()
	return name, parsed.OS()
}

func parseTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UnixMilli()
	}
	return 0
}
