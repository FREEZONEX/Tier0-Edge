package asset

import (
	"context"
	"errors"
	"testing"

	"backend/internal/infra/filestore"
)

// TestResolveMultipartPartSize 覆盖分片大小的归一化规则：≥5MB 采纳，否则回退默认 10MB。
func TestResolveMultipartPartSize(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int64
	}{
		{name: "zero falls back to default", in: 0, want: defaultMultipartPartSize},
		{name: "below minimum falls back to default", in: minMultipartPartSize - 1, want: defaultMultipartPartSize},
		{name: "exactly minimum is adopted", in: minMultipartPartSize, want: minMultipartPartSize},
		{name: "above minimum is adopted", in: 32 * 1024 * 1024, want: 32 * 1024 * 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMultipartPartSize(tc.in); got != tc.want {
				t.Fatalf("resolveMultipartPartSize(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestMultipartPartCount 覆盖分片数量边界：恰好整除、余数向上取整、小于单片的
// 文件算 1 片、1GB 上限下的分片数。
func TestMultipartPartCount(t *testing.T) {
	cases := []struct {
		name      string
		size      int64
		partSize  int64
		wantCount int64
	}{
		{name: "empty size counts one part", size: 0, partSize: defaultMultipartPartSize, wantCount: 1},
		{name: "file smaller than one part", size: 1024, partSize: defaultMultipartPartSize, wantCount: 1},
		{name: "exactly one part", size: defaultMultipartPartSize, partSize: defaultMultipartPartSize, wantCount: 1},
		{name: "one byte over one part rounds up", size: defaultMultipartPartSize + 1, partSize: defaultMultipartPartSize, wantCount: 2},
		{name: "exactly two parts", size: 2 * defaultMultipartPartSize, partSize: defaultMultipartPartSize, wantCount: 2},
		{name: "1GB with 10MB parts", size: maxOpenapiUploadSize, partSize: defaultMultipartPartSize, wantCount: 103},
		{name: "1GB with 5MB parts stays under S3 10000-part cap", size: maxOpenapiUploadSize, partSize: minMultipartPartSize, wantCount: 205},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := multipartPartCount(tc.size, tc.partSize); got != tc.wantCount {
				t.Fatalf("multipartPartCount(%d, %d) = %d, want %d", tc.size, tc.partSize, got, tc.wantCount)
			}
		})
	}
}

// TestValidateOpenapiMultipartInit 覆盖 init 入参校验：非法扩展名、越界大小、
// 非法 visibility 均拒绝；合法入参通过。
func TestValidateOpenapiMultipartInit(t *testing.T) {
	valid := OpenapiMultipartInitCommand{
		FileName:    "video.mp4",
		ContentType: "video/mp4",
		Size:        maxOpenapiUploadSize,
		Visibility:  VisibilityPublic,
		UseBy:       "user",
	}
	if err := validateOpenapiMultipartInit(valid); err != nil {
		t.Fatalf("valid init rejected: %v", err)
	}
	over := valid
	over.Size = maxOpenapiUploadSize + 1
	if err := validateOpenapiMultipartInit(over); err == nil {
		t.Fatalf("expect error for size over 1GB")
	}
	badExt := valid
	badExt.FileName = "evil.html"
	if err := validateOpenapiMultipartInit(badExt); err == nil {
		t.Fatalf("expect error for forbidden extension")
	}
	badVis := valid
	badVis.Visibility = "world"
	if err := validateOpenapiMultipartInit(badVis); err == nil {
		t.Fatalf("expect error for invalid visibility")
	}
	longSession := valid
	for i := 0; i < 70; i++ {
		longSession.SessionID += "a"
	}
	if err := validateOpenapiMultipartInit(longSession); err == nil {
		t.Fatalf("expect error for sessionId over 64 chars")
	}
}

// TestResolveMultipartTempValidation 覆盖导出解析器（供 core 分片 follow-up 权限复检使用）
// 的入参校验：fileKey/uploadId 任一为空或空白直接返回 ErrInvalid，不触达 DB。
func TestResolveMultipartTempValidation(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		fileKey  string
		uploadID string
	}{
		{name: "empty fileKey", fileKey: "", uploadID: "upload-1"},
		{name: "blank fileKey", fileKey: "  ", uploadID: "upload-1"},
		{name: "empty uploadId", fileKey: "key-1", uploadID: ""},
		{name: "blank uploadId", fileKey: "key-1", uploadID: " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ResolveMultipartTemp(ctx, tc.fileKey, tc.uploadID); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ResolveMultipartTemp err = %v, want ErrInvalid", err)
			}
		})
	}
}

// TestCoreMultipartInitValidation 覆盖 core 版 init 的参数校验：projectId 显式必填
// （不得为 0 或负数）、文件名/大小校验复用 openapi 规则（非法扩展名、越界大小拒绝）。
// 合法入参能通过校验门并进入对象存储调用（local 驱动返回 ErrMultipartUnsupported，
// 恰好证明已越过 projectId 与参数校验）。
func TestCoreMultipartInitValidation(t *testing.T) {
	// files 使用 local 驱动：校验通过后走到 CreateMultipartUpload 即返回不支持；
	// 不会触碰 repo/驱动其他字段，Service 其余成员保持零值即可。
	svc := &Service{files: &filestore.LocalStore{}}

	ctx := context.Background()
	valid := CoreMultipartInitCommand{
		ProjectID:   42,
		FileName:    "app-import.zip",
		ContentType: "application/zip",
		Size:        64 * 1024 * 1024,
		PartSize:    10 * 1024 * 1024,
	}

	t.Run("projectId zero rejected", func(t *testing.T) {
		cmd := valid
		cmd.ProjectID = 0
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(projectID=0) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("projectId negative rejected", func(t *testing.T) {
		cmd := valid
		cmd.ProjectID = -1
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(projectID=-1) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("forbidden extension rejected", func(t *testing.T) {
		cmd := valid
		cmd.FileName = "evil.html"
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(forbidden ext) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("empty file name rejected", func(t *testing.T) {
		cmd := valid
		cmd.FileName = ""
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(empty name) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("zero size rejected", func(t *testing.T) {
		cmd := valid
		cmd.Size = 0
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(size=0) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("size over 1GB rejected", func(t *testing.T) {
		cmd := valid
		cmd.Size = maxOpenapiUploadSize + 1
		if _, err := svc.CoreMultipartInit(ctx, cmd); !errors.Is(err, ErrInvalid) {
			t.Fatalf("CoreMultipartInit(size over 1GB) err = %v, want ErrInvalid", err)
		}
	})
	t.Run("valid command passes the validation gate", func(t *testing.T) {
		// local 驱动不支持分片，合法入参应到达对象存储调用并返回 ErrMultipartUnsupported，
		// 而非参数校验错误。
		_, err := svc.CoreMultipartInit(ctx, valid)
		if !errors.Is(err, ErrMultipartUnsupported) {
			t.Fatalf("CoreMultipartInit(valid) err = %v, want ErrMultipartUnsupported", err)
		}
	})
}
