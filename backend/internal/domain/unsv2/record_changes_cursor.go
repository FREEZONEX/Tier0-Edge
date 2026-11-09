package unsv2

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

const recordChangesCursorPurpose = "tier0/uns-v2/record-subscribe/cursor/v2"
const recordChangesCursorPrefix = "rc2_"
const recordChangesMaxCursor = 8192
const recordChangesCursorPlaintextBytes = 512

// recordChangesCursor 是续传位置 {Binding, (xid, seq)} 与库实例标识；过期按位置与已清理下界判断，不看签发时间
type recordChangesCursor struct {
	Binding  string `json:"binding"`
	XID      string `json:"xid"`
	Seq      int64  `json:"seq"`
	Database string `json:"db"`
}

func (s *Service) SetRecordChangesCursorSecret(secret string) {
	s.recordChangesKey = nil
	if secret == "" {
		return
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(recordChangesCursorPurpose))
	s.recordChangesKey = h.Sum(nil)
}

func (s *Service) recordChangesAEAD() (cipher.AEAD, error) {
	if len(s.recordChangesKey) != sha256.Size {
		return nil, problem("TEMPORARILY_UNAVAILABLE", "record notification cursor unavailable")
	}
	block, err := aes.NewCipher(s.recordChangesKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *Service) sealRecordChangesCursor(value recordChangesCursor) (string, error) {
	aead, err := s.recordChangesAEAD()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(raw) > recordChangesCursorPlaintextBytes {
		return "", invalid("cursor", "record notification cursor exceeds budget")
	}
	// 固定长度，避免通过密文长度推断全流序号位数。
	raw = append(raw, []byte(strings.Repeat(" ", recordChangesCursorPlaintextBytes-len(raw)))...)
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", problem("TEMPORARILY_UNAVAILABLE", "record notification cursor unavailable")
	}
	sealed := aead.Seal(nonce, nonce, raw, []byte(recordChangesCursorPurpose))
	return recordChangesCursorPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// openRecordChangesCursor 解开游标；格式不对是参数错误，格式正确但解不开（密钥轮换、被篡改）返回 stale，按 CURSOR_EXPIRED reset
func (s *Service) openRecordChangesCursor(token string) (recordChangesCursor, bool, error) {
	var value recordChangesCursor
	bad := func() (recordChangesCursor, bool, error) {
		return recordChangesCursor{}, false, invalid("cursor", "invalid record subscription cursor")
	}
	if len(token) > recordChangesMaxCursor || !strings.HasPrefix(token, recordChangesCursorPrefix) {
		return bad()
	}
	aead, err := s.recordChangesAEAD()
	if err != nil {
		return value, false, err
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, recordChangesCursorPrefix))
	if err != nil || len(sealed) != aead.NonceSize()+aead.Overhead()+recordChangesCursorPlaintextBytes {
		return bad()
	}
	raw, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(recordChangesCursorPurpose))
	if err != nil {
		return value, true, nil
	}
	if Decode(raw, &value) != nil {

		return bad()
	}
	if value.Database == "" {
		return value, true, nil
	}
	if len(value.Binding) != sha256.Size*2 || value.Seq < 0 {
		return bad()
	}
	if xid, err := strconv.ParseUint(value.XID, 10, 64); err != nil || strconv.FormatUint(xid, 10) != value.XID {
		return bad()
	}
	return value, false, nil
}
