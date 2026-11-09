package repo

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrFleetQueueFull = errors.New("Fleet durable upload queue is full")

// Queue bytes include payload and immutable delivery configuration plus a row
// allowance. They are a bounded logical budget, not an estimate of WAL/indexes.
const fleetQueueRowAllowance int64 = 512
const fleetNodeQueueEvents int64 = 100000
const fleetNodeQueueDeliveries int64 = 200000
const fleetNodeQueueBytes int64 = 1024 * 1024 * 1024

type FleetQueueBudget struct {
	BudgetID      string `gorm:"primaryKey" json:"budgetId"`
	MaxEvents     int64  `json:"maxEvents"`
	MaxDeliveries int64  `json:"maxDeliveries"`
	MaxBytes      int64  `json:"maxBytes"`
}
type FleetQueueUsage struct {
	Events     int64 `json:"events"`
	Deliveries int64 `json:"deliveries"`
	Bytes      int64 `json:"estimatedBytes"`
}

func defaultFleetQueueBudget(id string) FleetQueueBudget {
	return FleetQueueBudget{BudgetID: id, MaxEvents: fleetNodeQueueEvents, MaxDeliveries: fleetNodeQueueDeliveries, MaxBytes: fleetNodeQueueBytes}
}
func lockFleetQueueBudget(tx *gorm.DB, table, id string) (FleetQueueBudget, error) {
	budget := defaultFleetQueueBudget(id)
	if err := tx.Table(table).Clauses(clause.OnConflict{DoNothing: true}).Create(&budget).Error; err != nil {
		return budget, err
	}
	err := tx.Table(table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("budget_id=?", id).Take(&budget).Error
	return budget, err
}
func (u FleetQueueUsage) exceeds(b FleetQueueBudget) bool {
	return u.Events > b.MaxEvents || u.Deliveries > b.MaxDeliveries || u.Bytes > b.MaxBytes
}
