package unsv2

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON accepts Nova's ordered [field, direction] pair. The object form
// is retained only for deprecated orderBy requests and persisted old receipts.
func (o *RecordOrder) UnmarshalJSON(raw []byte) error {
	var pair []json.RawMessage
	if json.Unmarshal(raw, &pair) == nil {
		if len(pair) != 2 || json.Unmarshal(pair[0], &o.Field) != nil {
			return fmt.Errorf("sort must be [[field,1|-1],...]")
		}
		var direction int
		if json.Unmarshal(pair[1], &direction) != nil || direction != 1 && direction != -1 {
			return fmt.Errorf("sort direction must be 1 or -1")
		}
		o.Direction = "asc"
		if direction < 0 {
			o.Direction = "desc"
		}
		return nil
	}
	type plain RecordOrder
	var value plain
	if err := Decode(raw, &value); err != nil {
		return err
	}
	*o = RecordOrder(value)
	return nil
}
func (o RecordOrder) MarshalJSON() ([]byte, error) {
	direction := 1
	if o.Direction == "desc" {
		direction = -1
	}
	return json.Marshal([]any{o.Field, direction})
}
