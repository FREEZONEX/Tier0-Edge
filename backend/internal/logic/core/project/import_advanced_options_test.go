package project

import (
	"context"
	"errors"
	"testing"

	"backend/internal/repo"
	"backend/internal/service/tenantpg"
	"backend/internal/types"
)

// normalizeSchemaName 用例：UUID 去连字符、非法字符替换、系统名拒绝、超长截断
func TestNormalizeSchemaName(t *testing.T) {
	cases := []struct {
		name  string
		appID string
		want  string
	}{
		{
			name:  "UUID 去连字符",
			appID: "123e4567-e89b-12d3-a456-426614174000",
			want:  "app_123e4567e89b12d3a456426614174000",
		},
		{
			name:  "大写转小写",
			appID: "ABC-DEF",
			want:  "app_abcdef",
		},
		{
			name:  "点/斜杠转下划线",
			appID: "a.b/c",
			want:  "app_a_b_c",
		},
		{
			name:  "空串返回空",
			appID: "",
			want:  "",
		},
		{
			name:  "空白串返回空",
			appID: "   ",
			want:  "",
		},
		{
			name:  "appId 为 public 时加前缀仍合法（app_public 非系统名）",
			appID: "public",
			want:  "app_public",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSchemaName(tc.appID)
			if got != tc.want {
				t.Fatalf("normalizeSchemaName(%q) = %q, want %q", tc.appID, got, tc.want)
			}
		})
	}
}

// normalizeSchemaName 结果必须通过 tenantpg.ValidateIdentifier（合法 PG 标识符）
func TestNormalizeSchemaName_ProducesValidIdentifier(t *testing.T) {
	uuids := []string{
		"123e4567-e89b-12d3-a456-426614174000",
		"ABCDEF12-3456-7890-abcd-ef1234567890",
		"app-1",
	}
	for _, appID := range uuids {
		got := normalizeSchemaName(appID)
		if got == "" {
			continue // 允许回退
		}
		if err := tenantpg.ValidateIdentifier(got); err != nil {
			t.Fatalf("normalizeSchemaName(%q) = %q invalid identifier: %v", appID, got, err)
		}
		if len(got) > 63 {
			t.Fatalf("normalizeSchemaName(%q) too long: %d", appID, len(got))
		}
	}
}

// isReservedSystemSchema 系统名拒绝
func TestIsReservedSystemSchema(t *testing.T) {
	for _, name := range []string{"public", "information_schema", "pg_catalog", "pg_toast", "template1", "template0"} {
		if !isReservedSystemSchema(name) {
			t.Fatalf("expected %q to be reserved", name)
		}
	}
	for _, name := range []string{"app_123", "my_schema", "custom"} {
		if isReservedSystemSchema(name) {
			t.Fatalf("expected %q not to be reserved", name)
		}
	}
}

// resolvePreallocatedSchemaName 合法返回、非法/系统名/空返回空
func TestResolvePreallocatedSchemaName(t *testing.T) {
	if got := resolvePreallocatedSchemaName(context.Background(), "app_abc123", 1); got != "app_abc123" {
		t.Fatalf("valid schema rejected: %q", got)
	}
	if got := resolvePreallocatedSchemaName(context.Background(), "App-ABC", 1); got != "" {
		t.Fatalf("uppercase/hyphen schema should be rejected, got %q", got)
	}
	if got := resolvePreallocatedSchemaName(context.Background(), "public", 1); got != "" {
		t.Fatalf("system schema should be rejected, got %q", got)
	}
	if got := resolvePreallocatedSchemaName(context.Background(), "", 1); got != "" {
		t.Fatalf("empty schema should pass through empty, got %q", got)
	}
}

func TestResolveImportSchemaName(t *testing.T) {
	if got := resolveImportSchemaName(context.Background(), "app_custom", "123e4567-e89b-12d3-a456-426614174000", 1); got != "app_custom" {
		t.Fatalf("explicit schema should win, got %q", got)
	}
	if got := resolveImportSchemaName(context.Background(), "", "123e4567-e89b-12d3-a456-426614174000", 1); got != "app_123e4567e89b12d3a456426614174000" {
		t.Fatalf("empty schema should fall back to meta appId, got %q", got)
	}
	if got := resolveImportSchemaName(context.Background(), "", "", 1); got != "" {
		t.Fatalf("no schema and no appId should stay empty, got %q", got)
	}
	if got := resolveImportSchemaName(context.Background(), "public", "123e4567-e89b-12d3-a456-426614174000", 1); got != "app_123e4567e89b12d3a456426614174000" {
		t.Fatalf("rejected explicit schema should fall back to meta appId, got %q", got)
	}
}

// normalizeEnvVars：重复 key / 非法 key / 平台强制变量 / 数量与长度上限
func TestNormalizeEnvVars(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		env, err := normalizeEnvVars([]types.EnvVarItem{{Key: "FOO", Value: "bar"}, {Key: "BAZ", Value: "1"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env["FOO"] != "bar" || env["BAZ"] != "1" {
			t.Fatalf("unexpected env: %+v", env)
		}
	})

	t.Run("duplicate key", func(t *testing.T) {
		if _, err := normalizeEnvVars([]types.EnvVarItem{{Key: "FOO", Value: "1"}, {Key: "FOO", Value: "2"}}); err == nil {
			t.Fatal("expected duplicate key error")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		if _, err := normalizeEnvVars([]types.EnvVarItem{{Key: "1FOO", Value: "1"}}); err == nil {
			t.Fatal("expected invalid key error")
		}
		if _, err := normalizeEnvVars([]types.EnvVarItem{{Key: "FOO-BAR", Value: "1"}}); err == nil {
			t.Fatal("expected invalid key error")
		}
		if _, err := normalizeEnvVars([]types.EnvVarItem{{Key: "  ", Value: "1"}}); err == nil {
			t.Fatal("expected empty key error")
		}
	})

	t.Run("platform forced var rejected", func(t *testing.T) {
		for _, key := range []string{"DATABASE_URL", "DB_SCHEMA", "PORT", "HOSTNAME", "APP_ID", "TIER0_API_KEY"} {
			if _, err := normalizeEnvVars([]types.EnvVarItem{{Key: key, Value: "x"}}); err == nil {
				t.Fatalf("expected platform var %s rejected", key)
			}
		}
	})

	t.Run("too many", func(t *testing.T) {
		items := make([]types.EnvVarItem, maxEnvVarCount+1)
		for i := range items {
			items[i] = types.EnvVarItem{Key: "K" + string(rune('a'+i%26)) + "X", Value: "v"}
		}
		if _, err := normalizeEnvVars(items); err == nil {
			t.Fatal("expected too many error")
		}
	})
}

func TestBuildEditableEnvVarItems_FiltersPlatformForcedVars(t *testing.T) {
	items := buildEditableEnvVarItems(map[string]string{
		"APP_ID":       "cloud-app-id",
		"DATABASE_URL": "postgres://package-db",
		"CUSTOM_ENV":   "value",
	})

	if len(items) != 1 || items[0].Key != "CUSTOM_ENV" || items[0].Value != "value" {
		t.Fatalf("expected only user-editable env vars, got %+v", items)
	}
}

// resolveArtifactEnv：replace 用用户列表、inherit/空用包内
func TestResolveArtifactEnv(t *testing.T) {
	pkgEnv := map[string]string{"A": "1", "B": "2"}
	userEnv := map[string]string{"B": "3", "C": "4"}

	if got := resolveArtifactEnv(pkgEnv, "replace", userEnv); len(got) != 2 || got["B"] != "3" || got["C"] != "4" {
		t.Fatalf("replace should use user env, got %+v", got)
	}
	if got := resolveArtifactEnv(pkgEnv, "", nil); len(got) != 2 || got["A"] != "1" {
		t.Fatalf("empty policy should use pkg env, got %+v", got)
	}
	if got := resolveArtifactEnv(pkgEnv, "inherit", nil); len(got) != 2 || got["A"] != "1" {
		t.Fatalf("inherit should use pkg env, got %+v", got)
	}
}

// buildImportOptions：schema 校验 + env 校验
func TestBuildImportOptions(t *testing.T) {
	t.Run("empty ok", func(t *testing.T) {
		opts, err := buildImportOptions("", "", nil, "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.schemaName != "" || opts.envPolicy != "" {
			t.Fatalf("unexpected opts: %+v", opts)
		}
	})

	t.Run("valid schema", func(t *testing.T) {
		opts, err := buildImportOptions("app_abc123", "replace", nil, "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.schemaName != "app_abc123" {
			t.Fatalf("unexpected schema: %q", opts.schemaName)
		}
	})

	t.Run("system schema rejected", func(t *testing.T) {
		if _, err := buildImportOptions("public", "", nil, "", nil); err == nil {
			t.Fatal("expected system schema rejected")
		}
	})

	t.Run("invalid schema rejected", func(t *testing.T) {
		if _, err := buildImportOptions("App-ABC", "", nil, "", nil); err == nil {
			t.Fatal("expected invalid schema rejected")
		}
	})

	t.Run("replace env validated", func(t *testing.T) {
		opts, err := buildImportOptions("", "replace", []types.EnvVarItem{{Key: "FOO", Value: "1"}}, "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.envVars["FOO"] != "1" {
			t.Fatalf("unexpected envVars: %+v", opts.envVars)
		}
	})

	t.Run("replace forced env rejected", func(t *testing.T) {
		if _, err := buildImportOptions("", "replace", []types.EnvVarItem{{Key: "DATABASE_URL", Value: "x"}}, "", nil); err == nil {
			t.Fatal("expected forced env rejected")
		}
	})

	t.Run("inherit ignores envVars", func(t *testing.T) {
		opts, err := buildImportOptions("", "inherit", []types.EnvVarItem{{Key: "FOO", Value: "1"}}, "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts.envVars) != 0 {
			t.Fatalf("inherit should ignore envVars, got %+v", opts.envVars)
		}
	})
}

// checkAppSourceDuplicate 项目内查重：其他 Project 有相同 sourceAppId 不阻断、当前 Project 有返回 replace 决策
func TestCheckAppSourceDuplicate_ProjectScoped(t *testing.T) {
	ctx := context.Background()
	if repo.GetCommonConn(ctx) == nil {
		t.Skip("common DB not initialized")
	}

	// 同 Project 命中 → appReplaceRequiredError
	err := checkAppSourceDuplicate(ctx, 1, projectAppMeta{AppID: "app-same-project"})
	if err == nil {
		t.Fatal("expected replace required error for same project")
	}
	var replaceErr *appReplaceRequiredError
	if !errors.As(err, &replaceErr) {
		t.Fatalf("expected appReplaceRequiredError, got %T", err)
	}
}

// wrapProjectAppCreateError：ErrDuplicate 转为 ErrSchemaNameTaken
func TestWrapProjectAppCreateError(t *testing.T) {
	err := wrapProjectAppCreateError(repo.ErrDuplicate)
	if !errors.Is(err, ErrSchemaNameTaken) {
		t.Fatalf("expected ErrSchemaNameTaken, got %v", err)
	}
	other := wrapProjectAppCreateError(errors.New("boom"))
	if errors.Is(other, ErrSchemaNameTaken) {
		t.Fatal("non-duplicate error should not wrap ErrSchemaNameTaken")
	}
}

// normalizeVolumes 用例：合法 bind mount、空/相对/逃逸/非法字符路径、保留路径、重复容器路径、超数量
func TestNormalizeVolumes(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		vols, err := normalizeVolumes([]types.AppVolumeItem{
			{HostPath: "/srv/appdata", ContainerPath: "/data"},
			{HostPath: "/mnt/logs", ContainerPath: "/var/log/app", ReadOnly: true},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(vols) != 2 || vols[0].HostPath != "/srv/appdata" || vols[0].ContainerPath != "/data" || vols[0].ReadOnly {
			t.Fatalf("unexpected volumes: %+v", vols)
		}
		if !vols[1].ReadOnly {
			t.Fatalf("readonly flag lost: %+v", vols[1])
		}
	})

	t.Run("empty list ok", func(t *testing.T) {
		vols, err := normalizeVolumes(nil)
		if err != nil || vols != nil {
			t.Fatalf("empty should be nil,nil, got %+v, %v", vols, err)
		}
	})

	t.Run("empty or relative path rejected", func(t *testing.T) {
		for _, item := range []types.AppVolumeItem{
			{HostPath: "", ContainerPath: "/data"},
			{HostPath: "  ", ContainerPath: "/data"},
			{HostPath: "data", ContainerPath: "/data"},
			{HostPath: "/srv/a", ContainerPath: "data"},
		} {
			if _, err := normalizeVolumes([]types.AppVolumeItem{item}); err == nil {
				t.Fatalf("expected error for %+v", item)
			}
		}
	})

	t.Run("non-canonical or escape path rejected", func(t *testing.T) {
		for _, p := range []string{"/srv/../etc", "/srv//a", "/srv/./a", "/srv/a/"} {
			if _, err := normalizeVolumes([]types.AppVolumeItem{{HostPath: p, ContainerPath: "/data"}}); err == nil {
				t.Fatalf("expected error for hostPath %q", p)
			}
		}
	})

	t.Run("yaml-breaking chars rejected", func(t *testing.T) {
		for _, p := range []string{"/srv/a:b", "/srv/a\"b", "/srv/a\nb"} {
			if _, err := normalizeVolumes([]types.AppVolumeItem{{HostPath: p, ContainerPath: "/data"}}); err == nil {
				t.Fatalf("expected error for hostPath %q", p)
			}
		}
	})

	t.Run("reserved container path rejected", func(t *testing.T) {
		for _, p := range []string{"/", "/app/data", "/app/data/sub", "/proc", "/sys/fs", "/dev/null"} {
			if _, err := normalizeVolumes([]types.AppVolumeItem{{HostPath: "/srv/a", ContainerPath: p}}); err == nil {
				t.Fatalf("expected reserved container path %q rejected", p)
			}
		}
	})

	t.Run("host root rejected", func(t *testing.T) {
		if _, err := normalizeVolumes([]types.AppVolumeItem{{HostPath: "/", ContainerPath: "/data"}}); err == nil {
			t.Fatal("expected host root rejected")
		}
	})

	t.Run("duplicate container path rejected", func(t *testing.T) {
		if _, err := normalizeVolumes([]types.AppVolumeItem{
			{HostPath: "/srv/a", ContainerPath: "/data"},
			{HostPath: "/srv/b", ContainerPath: "/data"},
		}); err == nil {
			t.Fatal("expected duplicate container path error")
		}
	})

	t.Run("too many", func(t *testing.T) {
		items := make([]types.AppVolumeItem, maxVolumeCount+1)
		for i := range items {
			items[i] = types.AppVolumeItem{HostPath: "/srv/a", ContainerPath: "/data" + string(rune('a'+i%26)) + string(rune('a'+i/26))}
		}
		if _, err := normalizeVolumes(items); err == nil {
			t.Fatal("expected too many error")
		}
	})
}

// resolveArtifactVolumes：replace 用用户列表，inherit/空沿用既有列表
func TestResolveArtifactVolumes(t *testing.T) {
	user := []artifactVolume{{HostPath: "/srv/u", ContainerPath: "/data"}}
	existing := []artifactVolume{{HostPath: "/srv/e", ContainerPath: "/conf", ReadOnly: true}}

	if got := resolveArtifactVolumes("replace", user, existing); len(got) != 1 || got[0].HostPath != "/srv/u" {
		t.Fatalf("replace should use user volumes, got %+v", got)
	}
	if got := resolveArtifactVolumes("replace", nil, existing); got != nil {
		t.Fatalf("replace with empty user list should clear volumes, got %+v", got)
	}
	if got := resolveArtifactVolumes("inherit", user, existing); len(got) != 1 || got[0].HostPath != "/srv/e" {
		t.Fatalf("inherit should keep existing volumes, got %+v", got)
	}
	if got := resolveArtifactVolumes("", nil, nil); got != nil {
		t.Fatalf("empty policy without existing should be nil, got %+v", got)
	}
}

// buildImportOptions volumes：replace 校验生效，非 replace 忽略提交列表
func TestBuildImportOptionsVolumes(t *testing.T) {
	t.Run("replace volumes ok", func(t *testing.T) {
		opts, err := buildImportOptions("", "", nil, "replace", []types.AppVolumeItem{{HostPath: "/srv/a", ContainerPath: "/data"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(opts.volumes) != 1 || opts.volumes[0].ContainerPath != "/data" {
			t.Fatalf("unexpected opts.volumes: %+v", opts.volumes)
		}
	})

	t.Run("replace invalid volume rejected", func(t *testing.T) {
		if _, err := buildImportOptions("", "", nil, "replace", []types.AppVolumeItem{{HostPath: "rel", ContainerPath: "/data"}}); err == nil {
			t.Fatal("expected invalid volume error")
		}
	})

	t.Run("inherit ignores volumes", func(t *testing.T) {
		opts, err := buildImportOptions("", "", nil, "inherit", []types.AppVolumeItem{{HostPath: "rel", ContainerPath: "/data"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.volumes != nil {
			t.Fatalf("inherit should ignore volumes, got %+v", opts.volumes)
		}
	})
}

// manifestVolumes 读取持久化列表：缺省/null/异常格式均返回空切片
func TestManifestVolumes(t *testing.T) {
	if got := manifestVolumes(map[string]any{}); len(got) != 0 {
		t.Fatalf("missing key should be empty, got %+v", got)
	}
	if got := manifestVolumes(map[string]any{"volumes": nil}); len(got) != 0 {
		t.Fatalf("null should be empty, got %+v", got)
	}
	if got := manifestVolumes(map[string]any{"volumes": "bad"}); len(got) != 0 {
		t.Fatalf("bad format should be empty, got %+v", got)
	}
	got := manifestVolumes(map[string]any{"volumes": []any{
		map[string]any{"hostPath": "/srv/a", "containerPath": "/data", "readOnly": true},
	}})
	if len(got) != 1 || got[0].HostPath != "/srv/a" || !got[0].ReadOnly {
		t.Fatalf("unexpected volumes: %+v", got)
	}
	if len(volumesOrEmpty(nil)) != 0 {
		t.Fatal("volumesOrEmpty should normalize nil to empty slice")
	}
}
