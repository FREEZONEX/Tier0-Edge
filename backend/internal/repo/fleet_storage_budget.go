package repo

import (
	"errors"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"
)

var ErrFleetStorageFull = errors.New("Fleet physical storage admission limit reached")
var ErrFleetStorageUnobserved = errors.New("Fleet physical storage cannot be measured")

// These limits stop new Fleet payload admission, not PostgreSQL itself. WAL and
// temporary files are server-wide resources; unrelated writers and in-flight
// transactions still need a filesystem reserve and external disk monitoring.
type FleetStorageStatus struct {
	DatabaseBytes     int64  `json:"databaseBytes"`
	WALBytes          *int64 `json:"walBytes"`
	TemporaryBytes    *int64 `json:"temporaryBytes"`
	MaxDatabaseBytes  int64  `json:"maxDatabaseBytes"`
	MaxWALBytes       int64  `json:"maxWalBytes"`
	MaxTemporaryBytes int64  `json:"maxTemporaryBytes"`
	ReserveBytes      int64  `json:"reserveBytes"`
	MeasuredAt        int64  `json:"measuredAt"`
	Status            string `json:"status"`
}

func fleetStorageLimit(name string, fallback int64) (int64, bool) {
	value, exists := os.LookupEnv(name)
	if !exists {
		return fallback, true
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0
}

func FleetStorageUsage(db *gorm.DB) (FleetStorageStatus, error) {
	s := FleetStorageStatus{Status: "available"}
	valid := true
	for _, item := range []struct {
		name     string
		value    *int64
		fallback int64
	}{
		{"FLEET_STORAGE_MAX_DATABASE_BYTES", &s.MaxDatabaseBytes, 20 << 30},
		{"FLEET_STORAGE_MAX_WAL_BYTES", &s.MaxWALBytes, 4 << 30},
		{"FLEET_STORAGE_MAX_TEMPORARY_BYTES", &s.MaxTemporaryBytes, 1 << 30},
		{"FLEET_STORAGE_RESERVE_BYTES", &s.ReserveBytes, 128 << 20},
	} {
		n, ok := fleetStorageLimit(item.name, item.fallback)
		*item.value = n
		valid = valid && ok
	}
	if !valid {
		s.Status = "invalidConfiguration"
		return s, nil
	}
	// PostgreSQL checks function EXECUTE privileges even for an untaken CASE
	// branch. Inspect permissions first; never include an inaccessible function
	// in the next statement. NULL remains unknown, never zero.
	var permissions struct {
		Database  int64
		WAL       bool
		Temporary bool
	}
	err := db.Raw(`SELECT pg_database_size(current_database()) AS database,
 has_function_privilege(current_user,'pg_catalog.pg_ls_waldir()','EXECUTE') AS wal,
 has_function_privilege(current_user,'pg_catalog.pg_ls_tmpdir(oid)','EXECUTE') AS temporary`).Scan(&permissions).Error
	if err != nil {
		return s, err
	}
	s.DatabaseBytes = permissions.Database
	var usage struct {
		WAL       *int64
		Temporary *int64
	}
	wal, temporary := "NULL::bigint", "NULL::bigint"
	if permissions.WAL {
		wal = "(SELECT COALESCE(sum(size),0)::bigint FROM pg_catalog.pg_ls_waldir())"
	}
	if permissions.Temporary {
		temporary = "(SELECT COALESCE(sum(f.size),0)::bigint FROM pg_catalog.pg_tablespace t CROSS JOIN LATERAL pg_catalog.pg_ls_tmpdir(t.oid) f WHERE t.spcname <> 'pg_global')"
	}
	if permissions.WAL || permissions.Temporary {
		if err := db.Raw("SELECT " + wal + " AS wal," + temporary + " AS temporary").Scan(&usage).Error; err != nil {
			// Revocation may race the permission check, or a tablespace may be
			// inaccessible. New admission must still fail closed.
			usage.WAL, usage.Temporary = nil, nil
		}
	}
	s.WALBytes, s.TemporaryBytes = usage.WAL, usage.Temporary
	s.MeasuredAt = time.Now().UnixMilli()
	if usage.WAL == nil || usage.Temporary == nil {
		s.Status = "monitoringUnavailable"
	}
	if s.DatabaseBytes >= s.MaxDatabaseBytes-s.ReserveBytes ||
		usage.WAL != nil && *usage.WAL >= s.MaxWALBytes-s.ReserveBytes ||
		usage.Temporary != nil && *usage.Temporary >= s.MaxTemporaryBytes-s.ReserveBytes {
		s.Status = "capacityReached"
	}
	return s, nil
}

// Caller holds its normal logical queue lock. Check after duplicate receipts so
// a retry can recover its original ACK even while storage is full. Do not apply
// this gate to draining/acknowledgement/Control operations.
func CheckFleetStorageAdmission(db *gorm.DB) error {
	s, err := FleetStorageUsage(db)
	if err != nil {
		return ErrFleetStorageUnobserved
	}
	switch s.Status {
	case "available":
		return nil
	case "capacityReached":
		return ErrFleetStorageFull
	default:
		return ErrFleetStorageUnobserved
	}
}
