package unsv2

import (
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsquery"
	"errors"
	"gorm.io/gorm"
)

func recordQueryError(err error) error {
	var validation *unsquery.ValidationError
	if errors.As(err, &validation) {
		return invalid(validation.Path, validation.Message)
	}
	return err
}

func recordFilters(query *gorm.DB, schema *unsmeta.Schema, filters []RecordFilter) (*gorm.DB, error) {
	shared := make([]unsquery.RecordFilter, len(filters))
	for i, filter := range filters {
		shared[i] = unsquery.RecordFilter{Field: filter.Field, Op: filter.Op, Value: filter.Value}
	}
	result, err := unsquery.RecordFilters(query, schema, shared)
	return result, recordQueryError(err)
}

func recordOrder(query *gorm.DB, schema *unsmeta.Schema, orders []RecordOrder) (*gorm.DB, error) {
	shared := make([]unsquery.RecordOrder, len(orders))
	for i, order := range orders {
		shared[i] = unsquery.RecordOrder{Field: order.Field, Direction: order.Direction}
	}
	result, err := unsquery.OrderRecords(query, schema, shared)
	return result, recordQueryError(err)
}
