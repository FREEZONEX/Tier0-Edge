package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Sender 归一化后的发送方标识（对齐云端 sender 契约：自报值，仅展示，不可作安全判断）。
type Sender struct {
	Type   string
	ID     string
	Name   string
	Detail string // meta JSON 序列化（≤500 字节）
}

// NormalizedRequest 校验与归一化后的发送请求（六步链第①步产物）。
type NormalizedRequest struct {
	RecipientUserID int64
	Type            string
	Title           string
	Content         string
	Mode            string
	Channels        string // 归一化 "web,mobile"；空串=静默消息
	Sender          Sender
	Link            string
	ActionType      string // 有 link 时=link，否则空
	IdempotencyKey  string
}

// NormalizeChannels 渠道归一化：仅接受 web/mobile（大小写不敏感），固定序落库；
// 缺省/空数组=静默消息（空串），对齐云端 2026-08-24 契约修订。
func NormalizeChannels(in []string) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	has := map[string]bool{}
	for _, c := range in {
		switch strings.TrimSpace(strings.ToLower(c)) {
		case ChannelWeb:
			has[ChannelWeb] = true
		case ChannelMobile:
			has[ChannelMobile] = true
		default:
			return "", ErrInvalidRequest
		}
	}
	var parts []string
	if has[ChannelWeb] {
		parts = append(parts, ChannelWeb)
	}
	if has[ChannelMobile] {
		parts = append(parts, ChannelMobile)
	}
	return strings.Join(parts, ","), nil
}

// ValidateAndNormalize 六步链第①步：请求体本地校验与归一化（零 IO）。
// 校验顺序与规则逐条对齐云端 validateAndNormalize；失败返回 errs.ErrNotify* 哨兵。
func ValidateAndNormalize(recipientUserID, typ, title, content, idempotencyKey, source, mode, link string, channels []string, senderType, senderID, senderName string, senderMeta map[string]string) (*NormalizedRequest, error) {
	// 收件人：非空、纯数字、>0（大整数按 string 承载，此处解析回 int64）
	recipientUserID = strings.TrimSpace(recipientUserID)
	if recipientUserID == "" {
		return nil, ErrInvalidRequest
	}
	uid, err := strconv.ParseInt(recipientUserID, 10, 64)
	if err != nil || uid <= 0 {
		return nil, ErrInvalidRequest
	}

	// 类型：本期仅 inbox
	if strings.TrimSpace(typ) != TypeInbox {
		return nil, ErrInvalidType
	}

	// 标题/内容长度（按 rune 计，与云端 len(rune) 口径一致）
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > TitleMaxLength {
		return nil, ErrContentLimit
	}
	if len([]rune(content)) > ContentMaxLength {
		return nil, ErrContentLimit
	}

	// 幂等键：必填 ≤128
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > IdempotencyKeyMaxLength {
		return nil, ErrInvalidRequest
	}

	// 模式：test/live，空=live
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "":
		mode = ModeLive
	case ModeTest, ModeLive:
	default:
		return nil, ErrInvalidRequest
	}
	if mode == ModeTest {
		title = TestTitlePrefix + title
	}

	// 渠道归一化
	normalizedChannels, err := NormalizeChannels(channels)
	if err != nil {
		return nil, err
	}

	// sender 归一化：type 缺省 other；app 必填 id（字符集 [0-9a-zA-Z-]{1,128}）；
	// name 缺省由废弃字段 source 补位（sender.name 优先）；meta 序列化 ≤500 字节
	nSenderType := strings.TrimSpace(strings.ToLower(senderType))
	switch nSenderType {
	case "":
		nSenderType = SenderTypeOther
	case SenderTypeApp, SenderTypeOther:
	default:
		return nil, ErrInvalidRequest
	}
	if len(senderID) > SenderIDMaxLength || len(senderName) > SenderNameMaxLength {
		return nil, ErrInvalidRequest
	}
	if nSenderType == SenderTypeApp {
		if !validSenderAppID(senderID) {
			return nil, ErrInvalidRequest
		}
	}
	if senderName == "" {
		senderName = strings.TrimSpace(source)
		if len(senderName) > SourceMaxLength {
			return nil, ErrInvalidRequest
		}
	}
	detail, err := marshalSenderMeta(senderMeta)
	if err != nil {
		return nil, err
	}

	// link：支持企业内网 HTTP、HTTPS 或站内路径，≤500 字节，无空白/控制/不可见格式字符、无反斜杠；
	// //host 及反斜杠变体视为跨站绝对地址拒绝。
	// 不做 TrimSpace：合法 link 不含任何空白，首尾不可见字符本身即非法（对齐云端契约）
	if link != "" {
		if err := validateLink(link); err != nil {
			return nil, err
		}
	}

	return &NormalizedRequest{
		RecipientUserID: uid,
		Type:            TypeInbox,
		Title:           title,
		Content:         content,
		Mode:            mode,
		Channels:        normalizedChannels,
		Sender:          Sender{Type: nSenderType, ID: senderID, Name: senderName, Detail: detail},
		Link:            link,
		ActionType:      actionTypeFor(link),
		IdempotencyKey:  idempotencyKey,
	}, nil
}

// validSenderAppID app 发送方 id 字符集校验：[0-9a-zA-Z-]{1,128}（对齐云端）。
func validSenderAppID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

// marshalSenderMeta meta 序列化（JSON），超 500 字节拒绝；nil/空 → 空串。
func marshalSenderMeta(meta map[string]string) (string, error) {
	if len(meta) == 0 {
		return "", nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return "", ErrInvalidRequest
	}
	if len(data) > SenderDetailMaxBytes {
		return "", ErrInvalidRequest
	}
	return string(data), nil
}

// actionTypeFor 有跳转时落 link，无跳转双列留空（对齐云端）。
func actionTypeFor(link string) string {
	if link == "" {
		return ""
	}
	return ActionTypeLink
}

// validateLink 保留云端链接安全校验，并为企业内网应用兼容 HTTP 导航。
func validateLink(link string) error {
	if len(link) > LinkMaxLength {
		return ErrInvalidRequest
	}
	if strings.Contains(link, "\\") {
		return ErrInvalidRequest
	}
	if strings.HasPrefix(link, "//") {
		// 协议相对地址视为跨站绝对地址
		return ErrInvalidRequest
	}
	if !strings.HasPrefix(link, LinkSchemePrefix) && !strings.HasPrefix(link, LinkHTTPPrefix) && !strings.HasPrefix(link, "/") {
		return ErrInvalidRequest
	}
	for _, r := range link {
		// 空白（含 U+00A0 NBSP）/控制/不可见格式字符（U+200B、U+FEFF 等）一律拒绝
		if r <= 0x20 || r == 0x7f || unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ErrInvalidRequest
		}
	}
	if !strings.HasPrefix(link, "/") {
		target, err := url.Parse(link)
		if err != nil || target.Hostname() == "" || target.User != nil {
			return ErrInvalidRequest
		}
		// net/url 不限制数字端口范围，旧 Go 版本还可能接受未加方括号的多冒号主机。
		if strings.HasPrefix(target.Host, "[") {
			address, err := netip.ParseAddr(target.Hostname())
			if err != nil || !address.Is6() || address.Zone() != "" {
				return ErrInvalidRequest
			}
		} else if strings.Contains(target.Hostname(), ":") {
			return ErrInvalidRequest
		}
		if port := target.Port(); port != "" {
			if _, err := strconv.ParseUint(port, 10, 16); err != nil {
				return ErrInvalidRequest
			}
		}
	}
	return nil
}

// ComputeDigest 幂等内容摘要：sha256(固定字段序 JSON) hex（对齐云端 digest.go，
// 字段集=除 idempotencyKey 外的全部业务字段；空身份 other 不参与，link 空不参与）。
func ComputeDigest(n *NormalizedRequest) (string, error) {
	type digestPayload struct {
		RecipientUserID int64  `json:"recipientUserId"`
		Type            string `json:"type"`
		Title           string `json:"title"`
		Content         string `json:"content"`
		Source          string `json:"source"`
		Mode            string `json:"mode"`
		Channels        string `json:"channels"`
		SenderType      string `json:"senderType,omitempty"`
		SenderID        string `json:"senderId,omitempty"`
		SenderDetail    string `json:"senderDetail,omitempty"`
		Link            string `json:"link,omitempty"`
	}
	p := digestPayload{
		RecipientUserID: n.RecipientUserID,
		Type:            n.Type,
		Title:           n.Title,
		Content:         n.Content,
		Source:          n.Sender.Name,
		Mode:            n.Mode,
		Channels:        n.Channels,
		Link:            n.Link,
	}
	// 空身份 other（未传 sender 的缺省归一化结果）不进 digest
	if !(n.Sender.Type == SenderTypeOther && n.Sender.ID == "" && n.Sender.Detail == "") {
		p.SenderType = n.Sender.Type
		p.SenderID = n.Sender.ID
		p.SenderDetail = n.Sender.Detail
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
