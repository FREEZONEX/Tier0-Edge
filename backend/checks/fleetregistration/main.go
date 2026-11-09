// Registration persistence smoke against an explicitly isolated PostgreSQL DB.
// Does not establish HTTP authentication, licensing or cross-host acceptance.
package main

import (
	"backend/internal/repo"
	"context"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	dsn := os.Getenv("FLEET_SMOKE_DATABASE_URL")
	if dsn == "" {
		panic("FLEET_SMOKE_DATABASE_URL required; isolated database only")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err)
	var database string
	must(db.Raw("SELECT current_database()").Scan(&database).Error)
	if !strings.HasPrefix(database, "fleet_p1") {
		panic("refusing non-smoke database")
	}
	schema, err := os.ReadFile("internal/repo/migrations/schema.sql")
	must(err)
	text := string(schema)
	a := strings.Index(text, "CREATE TABLE IF NOT EXISTS fleet_node (")
	b := strings.Index(text[a:], "CREATE TABLE IF NOT EXISTS fleet_container_snapshot (") + a
	must(db.Exec(text[a:b]).Error)
	a = strings.Index(text, "CREATE TABLE IF NOT EXISTS fleet_quota_lock (")
	b = strings.Index(text[a:], "ON CONFLICT (id) DO NOTHING;") + a + len("ON CONFLICT (id) DO NOTHING;")
	must(db.Exec(text[a:b]).Error)
	migration, err := os.ReadFile("internal/repo/migrations/versions/common/V3.0.0_003__fleet_collaboration.sql")
	must(err)
	must(db.Exec(string(migration)).Error)
	must(db.Exec(string(migration)).Error)
	grantMigration, err := os.ReadFile("internal/repo/migrations/versions/common/V3.0.0_004__fleet_service_grants.sql")
	must(err)
	must(db.Exec(string(grantMigration)).Error)
	ctx := context.Background()
	r := repo.NewFleetRepo(db)
	id := "fn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	node, err := r.CreateCollaborationNode(ctx, repo.CreateFleetNodeInput{FleetNodeID: id, Name: id, BranchLimit: -1})
	must(err)
	if node.ConnectClientID != 0 {
		panic("unexpected MQTT identity")
	}
	view, err := r.CollaborationNode(ctx, id)
	must(err)
	if view.RegistrationStatus != "unregistered" {
		panic("unexpected registration")
	}
	now := time.Now().UnixMilli()
	hash := uuid.NewString()
	proof := uuid.NewString()
	network := uuid.NewString()
	must(r.CreateCollaborationEnrollment(ctx, repo.FleetCollaborationEnrollment{CodeHash: hash, NodeID: id, ExpiresAt: now + 60000}))
	registered := repo.FleetCollaborationNode{NodeID: id, PublicKey: uuid.NewString(), KeyID: uuid.NewString(), Endpoint: "https://edge.invalid", NetworkNodeID: &network}
	first, err := r.EnrollCollaborationNode(ctx, hash, proof, registered, now)
	must(err)
	again, err := r.EnrollCollaborationNode(ctx, hash, proof, registered, now)
	must(err)
	if first.NodeID != again.NodeID || first.RegisteredAt != again.RegisteredAt {
		panic("replay diverged")
	}
	if _, err = r.EnrollCollaborationNode(ctx, hash, "different-proof", registered, now); err == nil {
		panic("changed proof accepted")
	}
	if _, err = r.EnrollCollaborationNode(ctx, hash, proof, registered, now+60001); err != nil {
		panic("durable signed receipt unavailable after expiry")
	}
	expiredID := "fn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = r.CreateCollaborationNode(ctx, repo.CreateFleetNodeInput{FleetNodeID: expiredID, Name: expiredID, BranchLimit: -1})
	must(err)
	expiredCode := uuid.NewString()
	must(r.CreateCollaborationEnrollment(ctx, repo.FleetCollaborationEnrollment{CodeHash: expiredCode, NodeID: expiredID, ExpiresAt: now - 1}))
	expiredNode := registered
	expiredNode.NodeID = expiredID
	if _, err = r.EnrollCollaborationNode(ctx, expiredCode, proof, expiredNode, now); err == nil {
		panic("expired first consumption accepted")
	}

	// Reconnect to prove persistence is not a process-local registry.
	sqlDB, err := db.DB()
	must(err)
	must(sqlDB.Close())
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err)
	persisted, err := repo.NewFleetRepo(db).CollaborationNode(ctx, id)
	must(err)
	if persisted.RegistrationStatus != "registered" || persisted.PublicKey != registered.PublicKey {
		panic("identity not durable")
	}
	fmt.Println("PASS repeated migration, no MQTT side effects, durable identity, same-proof replay after expiry, changed-proof rejection, expired first-use rejection")
}
