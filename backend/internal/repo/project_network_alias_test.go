// 测试目标：内网别名创建、跨项目预占及部署状态的事务一致性。
// 正常路径：空别名、保留 manifest、同 App 重用、删除后释放；异常路径：重名、并发抢占、无效 JSON、CAS 失败。
// 使用 repo_test 避免 testkit/svctest 对 repo 的导入环，每个测试独立 PG 库，无外部业务服务。
package repo_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func aliasTestApp(projectID int64, key string, manifest json.RawMessage) repo.ProjectApp {
	return repo.ProjectApp{ProjectID: projectID, AppKey: key, Name: key, SiteType: "dynamic", Status: "active", ManifestJSON: manifest}
}

func TestProjectAppPrincipalIdentity(t *testing.T) {
	service := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := service.App.Store.CommonDB()
	r := repo.NewProjectRepo(db)
	firstProject := testkit.Insert(t, db, &repo.Project{ProjectKey: "principal-one", Name: "One", Status: "active", OwnerUserID: 1})
	secondProject := testkit.Insert(t, db, &repo.Project{ProjectKey: "principal-two", Name: "Two", Status: "active", OwnerUserID: 1})
	sourceID := uuid.NewString()
	manifest := json.RawMessage(fmt.Sprintf(`{"sourceAppId":%q,"app_id":%q}`, sourceID, sourceID))
	first, err := r.CreateProjectApp(ctx, aliasTestApp(firstProject.ID, "first", manifest))
	require.NoError(t, err)
	principal, err := uuid.Parse(first.AppPrincipalID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, principal)
	require.NotEqual(t, sourceID, first.AppPrincipalID)
	copy := aliasTestApp(secondProject.ID, "copy", manifest)
	copy.AppPrincipalID = first.AppPrincipalID
	second, err := r.CreateProjectAppWithNetworkAlias(ctx, copy, "copy-principal-api")
	require.NoError(t, err)
	require.NotEmpty(t, second.AppPrincipalID)
	require.NotEqual(t, first.AppPrincipalID, second.AppPrincipalID, "same package cannot share an installation's authority")
	resolved, err := r.GetProjectAppByPrincipalID(ctx, first.AppPrincipalID)
	require.NoError(t, err)
	require.Equal(t, first.ID, resolved.ID)
	require.Equal(t, firstProject.ID, resolved.ProjectID)
	reserved, err := r.ProjectAppPrincipalReserved(ctx, first.AppPrincipalID)
	require.NoError(t, err)
	require.True(t, reserved)
	for _, invalid := range []string{"", "1", sourceID[:8], strings.ToUpper(first.AppPrincipalID), uuid.Nil.String()} {
		_, err = r.GetProjectAppByPrincipalID(ctx, invalid)
		require.ErrorIs(t, err, repo.ErrInvalidArgument)
	}
	_, err = r.GetProjectAppByPrincipalID(ctx, sourceID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	for _, id := range []string{"", "1", uuid.Nil.String(), sourceID} {
		reserved, err := r.ProjectAppPrincipalReserved(ctx, id)
		require.NoError(t, err)
		require.False(t, reserved)
	}
	first.Name, first.AppPrincipalID = "Updated", second.AppPrincipalID
	updated, err := r.UpdateProjectApp(ctx, first.ID, first)
	require.NoError(t, err)
	require.Equal(t, resolved.AppPrincipalID, updated.AppPrincipalID, "replace/import updates preserve the installation identity")
	require.Error(t, db.Exec("UPDATE project_app SET app_principal_id = ? WHERE id = ?", uuid.NewString(), first.ID).Error,
		"the database rejects identity reassignment even outside the normal repo update")
	require.NoError(t, r.DeleteProjectApp(ctx, first.ProjectID, first.ID, 1))
	_, err = r.GetProjectAppByPrincipalID(ctx, resolved.AppPrincipalID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	reserved, err = r.ProjectAppPrincipalReserved(ctx, strings.ToUpper(resolved.AppPrincipalID))
	require.NoError(t, err)
	require.True(t, reserved, "deleted App identities remain reserved in every UUID spelling")
	replacement, err := r.CreateProjectApp(ctx, aliasTestApp(firstProject.ID, "replacement", manifest))
	require.NoError(t, err)
	require.NotEqual(t, resolved.AppPrincipalID, replacement.AppPrincipalID)
	require.NotEqual(t, second.AppPrincipalID, replacement.AppPrincipalID)
}

func TestProjectNetworkAlias_Create(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	r := repo.NewProjectRepo(db)
	p := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-create", Name: "alias-create", Status: "active", OwnerUserID: 1})

	app, err := r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "without-alias", nil), "")
	require.NoError(t, err)
	saved, err := r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(saved.ManifestJSON))

	app, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "empty-manifest", nil), "empty-api")
	require.NoError(t, err)
	saved, err = r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"network_alias":"empty-api"}`, string(saved.ManifestJSON))

	app, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "with-manifest", json.RawMessage(`{"container_port":3000,"env":{"MODE":"test"}}`)), "order-api")
	require.NoError(t, err)
	saved, err = r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"container_port":3000,"env":{"MODE":"test"},"network_alias":"order-api"}`, string(saved.ManifestJSON))

	_, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "invalid-json", json.RawMessage(`{`)), "retry-api")
	require.Error(t, err)
	_, err = r.GetProjectAppByKey(ctx, p.ID, "invalid-json")
	require.ErrorIs(t, err, repo.ErrNotFound)
	_, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "valid-retry", nil), "retry-api")
	require.NoError(t, err, "failed creation must release its alias")
}

func TestProjectNetworkAlias_Conflicts(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	r := repo.NewProjectRepo(db)
	p1 := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-one", Name: "alias-one", Status: "active", OwnerUserID: 1})
	p2 := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-two", Name: "alias-two", Status: "active", OwnerUserID: 1})
	for _, field := range []string{"network_alias", "network_alias_pending", "container_name"} {
		t.Run(field, func(t *testing.T) {
			manifest, err := json.Marshal(map[string]string{field: "taken-api"})
			require.NoError(t, err)
			owner, err := r.CreateProjectApp(ctx, aliasTestApp(p1.ID, "owner-"+field, manifest))
			require.NoError(t, err)
			_, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p2.ID, "contender-"+field, nil), "taken-api")
			require.ErrorIs(t, err, repo.ErrAppNetworkAliasTaken)
			_, err = r.GetProjectAppByKey(ctx, p2.ID, "contender-"+field)
			require.ErrorIs(t, err, repo.ErrNotFound)
			require.NoError(t, r.DeleteProjectApp(ctx, p1.ID, owner.ID, 1))
		})
	}
	_, err := r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p2.ID, "after-delete", nil), "taken-api")
	require.NoError(t, err, "deleted records must not reserve aliases")
}

func TestProjectNetworkAlias_ConcurrentCreate(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	r := repo.NewProjectRepo(db)
	p1 := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-race-one", Name: "alias-race-one", Status: "active", OwnerUserID: 1})
	p2 := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-race-two", Name: "alias-race-two", Status: "active", OwnerUserID: 1})
	start := make(chan struct{})
	results := make(chan error, 6)
	for i := 0; i < cap(results); i++ {
		go func(n int) {
			<-start
			projectID := p1.ID
			if n%2 == 0 {
				projectID = p2.ID
			}
			_, err := r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(projectID, fmt.Sprintf("contender-%d", n), nil), "shared-api")
			results <- err
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			require.ErrorIs(t, err, repo.ErrAppNetworkAliasTaken)
		} else {
			success++
		}
	}
	require.Equal(t, 1, success)
	var count int64
	require.NoError(t, db.Model(&repo.ProjectApp{}).Where("manifest_json->>'network_alias' = ?", "shared-api").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestProjectNetworkAlias_MarkDeploying(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	r := repo.NewProjectRepo(db)
	p := testkit.Insert(t, db, &repo.Project{ProjectKey: "alias-deploy", Name: "alias-deploy", Status: "active", OwnerUserID: 1})
	owner, err := r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "owner", nil), "taken-api")
	require.NoError(t, err)
	app, err := r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "target", json.RawMessage(`{"deploy_error":"previous failure"}`)), "old-api")
	require.NoError(t, err)

	require.ErrorIs(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, app.ID, 1, "taken-api"), repo.ErrAppNetworkAliasTaken)
	saved, err := r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.Equal(t, "active", saved.Status)
	require.JSONEq(t, string(app.ManifestJSON), string(saved.ManifestJSON))

	require.NoError(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, app.ID, 1, "new-api"))
	saved, err = r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.Equal(t, "deploying", saved.Status)
	require.JSONEq(t, `{"network_alias":"old-api","network_alias_pending":"new-api","deploying":true,"deploy_error":""}`, string(saved.ManifestJSON))
	for _, alias := range []string{"old-api", "new-api"} {
		require.ErrorIs(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, owner.ID, 1, alias), repo.ErrAppNetworkAliasTaken)
	}
	require.ErrorIs(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, app.ID, 1, "unused-api"), repo.ErrAppDeployInProgress)
	afterCAS, err := r.GetProjectApp(ctx, p.ID, app.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(saved.ManifestJSON), string(afterCAS.ManifestJSON))
	_, err = r.CreateProjectAppWithNetworkAlias(ctx, aliasTestApp(p.ID, "after-cas", nil), "unused-api")
	require.NoError(t, err, "failed CAS must not reserve the new alias")

	require.NoError(t, r.UpdateProjectAppStatus(ctx, p.ID, app.ID, "active", 1))
	require.NoError(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, app.ID, 1, "old-api"), "same App can reuse its alias")
	noAlias, err := r.CreateProjectApp(ctx, aliasTestApp(p.ID, "no-alias", nil))
	require.NoError(t, err)
	require.NoError(t, r.MarkAppDeployingWithNetworkAlias(ctx, p.ID, noAlias.ID, 1, ""))
}
