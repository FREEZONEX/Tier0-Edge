package notify

import (
	"strings"
	"testing"
)

// mustValid 内部快速构造合法请求。
func mustValid(t *testing.T, mutate func(*validateArgs)) *NormalizedRequest {
	t.Helper()
	args := validateArgs{
		recipient: "123", typ: TypeInbox, title: "hello", content: "world",
		idem: "key-1", mode: ModeLive,
	}
	if mutate != nil {
		mutate(&args)
	}
	n, err := ValidateAndNormalize(args.recipient, args.typ, args.title, args.content,
		args.idem, args.source, args.mode, args.link, args.channels,
		args.senderType, args.senderID, args.senderName, args.senderMeta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return n
}

type validateArgs struct {
	recipient, typ, title, content, idem, source, mode, link string
	channels                                                 []string
	senderType, senderID, senderName                         string
	senderMeta                                               map[string]string
}

func TestValidateAndNormalize_Legal(t *testing.T) {
	n := mustValid(t, nil)
	if n.RecipientUserID != 123 || n.Type != TypeInbox || n.Mode != ModeLive {
		t.Fatalf("basic normalize failed: %+v", n)
	}
	if n.Channels != "" {
		t.Fatalf("no channels should be silent, got %q", n.Channels)
	}
	if n.Sender.Type != SenderTypeOther {
		t.Fatalf("default sender type should be other, got %q", n.Sender.Type)
	}
	if n.ActionType != "" {
		t.Fatalf("no link should have empty action type")
	}
}

func TestValidateAndNormalize_Recipient(t *testing.T) {
	if _, err := ValidateAndNormalize("", TypeInbox, "t", "c", "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("empty recipient should be INVALID_REQUEST, got %v", err)
	}
	if _, err := ValidateAndNormalize("abc", TypeInbox, "t", "c", "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("non-numeric recipient should be INVALID_REQUEST, got %v", err)
	}
	if _, err := ValidateAndNormalize("0", TypeInbox, "t", "c", "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("zero recipient should be INVALID_REQUEST, got %v", err)
	}
}

func TestValidateAndNormalize_Type(t *testing.T) {
	if _, err := ValidateAndNormalize("1", "sms", "t", "c", "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005002) {
		t.Fatalf("invalid type should be INVALID_NOTIFICATION_TYPE, got %v", err)
	}
}

func TestValidateAndNormalize_Length(t *testing.T) {
	longTitle := strings.Repeat("标", TitleMaxLength+1)
	if _, err := ValidateAndNormalize("1", TypeInbox, longTitle, "c", "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005021) {
		t.Fatalf("over-length title should be CONTENT_LIMIT, got %v", err)
	}
	maxTitle := strings.Repeat("标", TitleMaxLength)
	if _, err := ValidateAndNormalize("1", TypeInbox, maxTitle, "c", "k", "", "", "", nil, "", "", "", nil); err != nil {
		t.Fatalf("max-length title should pass, got %v", err)
	}
	longContent := strings.Repeat("c", ContentMaxLength+1)
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", longContent, "k", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005021) {
		t.Fatalf("over-length content should be CONTENT_LIMIT, got %v", err)
	}
}

func TestValidateAndNormalize_IdempotencyKey(t *testing.T) {
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "", "", "", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("empty idempotency key should be INVALID_REQUEST, got %v", err)
	}
	longKey := strings.Repeat("k", IdempotencyKeyMaxLength+1)
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", longKey, "", "", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("over-length key should be INVALID_REQUEST, got %v", err)
	}
}

func TestValidateAndNormalize_Mode(t *testing.T) {
	n := mustValid(t, func(a *validateArgs) { a.mode = ModeTest; a.title = "hello" })
	if !strings.HasPrefix(n.Title, TestTitlePrefix) {
		t.Fatalf("test mode should prefix title, got %q", n.Title)
	}
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "beta", "", nil, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("invalid mode should be INVALID_REQUEST, got %v", err)
	}
}

func TestValidateAndNormalize_Channels(t *testing.T) {
	n := mustValid(t, func(a *validateArgs) { a.channels = []string{"web", "mobile"} })
	if n.Channels != ChannelsNormalized {
		t.Fatalf("channels normalize failed: %q", n.Channels)
	}
	n = mustValid(t, func(a *validateArgs) { a.channels = []string{"mobile", "web", "MOBILE"} })
	if n.Channels != ChannelsNormalized {
		t.Fatalf("channels dedupe+order failed: %q", n.Channels)
	}
	n = mustValid(t, func(a *validateArgs) { a.channels = []string{} })
	if n.Channels != "" {
		t.Fatalf("empty channels should be silent: %q", n.Channels)
	}
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", "", []string{"sms"}, "", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("unknown channel should be INVALID_REQUEST, got %v", err)
	}
}

func TestValidateAndNormalize_Sender(t *testing.T) {
	n := mustValid(t, func(a *validateArgs) { a.senderType = "app"; a.senderID = "app-123"; a.senderName = "MyApp" })
	if n.Sender.Type != "app" || n.Sender.ID != "app-123" || n.Sender.Name != "MyApp" {
		t.Fatalf("sender normalize failed: %+v", n.Sender)
	}
	// app 缺 id 非法
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", "", nil, "app", "", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("app sender without id should fail, got %v", err)
	}
	// id 字符集越界
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", "", nil, "app", "bad_id!", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("app id charset should fail, got %v", err)
	}
	// 非法 type
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", "", nil, "bot", "id", "", nil); !IsCode(err, 3005001) {
		t.Fatalf("unknown sender type should fail, got %v", err)
	}
	// source 兜底为 sender.name
	n = mustValid(t, func(a *validateArgs) { a.source = "LegacySource" })
	if n.Sender.Name != "LegacySource" {
		t.Fatalf("source should backfill sender.name, got %q", n.Sender.Name)
	}
	// sender.name 优先于 source
	n = mustValid(t, func(a *validateArgs) { a.source = "LegacySource"; a.senderName = "NewName" })
	if n.Sender.Name != "NewName" {
		t.Fatalf("sender.name should win over source, got %q", n.Sender.Name)
	}
	// meta 超字节
	big := map[string]string{"k": strings.Repeat("v", 600)}
	if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", "", nil, "app", "id", "", big); !IsCode(err, 3005001) {
		t.Fatalf("over-byte meta should fail, got %v", err)
	}
}

func TestValidateAndNormalize_Link(t *testing.T) {
	legal := []string{
		"https://example.com/a", "/relative/path", "https://example.com/a?b=1&c=中文",
		"http://example.com", "http://192.168.1.10:8080/app?tab=通知", "http://app.internal:3000/",
		"http://[::1]:8080/app",
		"http://internal:0/app", "http://internal:65535/app", "https://[2001:db8::1]:65535/app",
	}
	for _, l := range legal {
		if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", l, nil, "", "", "", nil); err != nil {
			t.Fatalf("legal link %q should pass, got %v", l, err)
		}
	}
	illegal := []string{
		"http://internal:abc", "http://internal:80:90", "http://internal:65536",
		"http://internal:-1", "http://internal:9999999999999999999999",
		"http://[::1]:65536", "http://[internal]:8080", "http://[fe80::1%25eth0]:8080",
		"http://",                            // 缺少主机
		"https://",                           // 缺少主机
		"http://user:password@internal/app",  // 禁止 URL 中的凭据
		"http://@internal/app",               // 空凭据同样拒绝
		"javascript:alert(1)",                // 禁止可执行协议
		"data:text/html,hello",               // 禁止 data URL
		"http://internal/a\u200b",            // HTTP 同样禁止不可见字符
		"//host/path",                        // 协议相对
		"ftp://example.com",                  // 非法协议
		`https://example.com/\evil`,          // 反斜杠
		"https://example.com/a b",            // 空格
		"https://example.com/\u200b",         // 零宽字符
		"https://example.com/\u00a0",         // NBSP
		strings.Repeat("https://x.com/", 60), // 超 500
	}
	for _, l := range illegal {
		if _, err := ValidateAndNormalize("1", TypeInbox, "t", "c", "k", "", "", l, nil, "", "", "", nil); err == nil {
			t.Fatalf("illegal link %q should fail", l)
		}
	}
	// 合法 link 带 action_type=link
	n := mustValid(t, func(a *validateArgs) { a.link = "https://example.com" })
	if n.ActionType != ActionTypeLink {
		t.Fatalf("link should set action type, got %q", n.ActionType)
	}
}

func TestComputeDigest(t *testing.T) {
	base := mustValid(t, nil)
	d1, err := ComputeDigest(base)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	// 同输入同摘要
	base2 := mustValid(t, nil)
	d2, _ := ComputeDigest(base2)
	if d1 != d2 {
		t.Fatal("digest should be deterministic")
	}
	// 内容变化 → 摘要变化
	changed := mustValid(t, func(a *validateArgs) { a.content = "changed" })
	d3, _ := ComputeDigest(changed)
	if d1 == d3 {
		t.Fatal("content change must change digest")
	}
	// 空身份 other 与未传 sender 判等（digest 不变）
	withOther := mustValid(t, func(a *validateArgs) { a.senderType = SenderTypeOther })
	d4, _ := ComputeDigest(withOther)
	if d1 != d4 {
		t.Fatal("default other sender should not affect digest")
	}
	// app sender 参与 digest
	withApp := mustValid(t, func(a *validateArgs) { a.senderType = "app"; a.senderID = "app-1" })
	d5, _ := ComputeDigest(withApp)
	if d1 == d5 {
		t.Fatal("app sender must affect digest")
	}
}
