package uns

import (
	"context"
	"errors"
	"testing"
)

func TestSourceStreamModeCannotCreateNodeREDTasks(t *testing.T) {
	s := &Service{}
	s.DisableLegacyFlows()
	for _, command := range []SaveCommand{{AddFlow: true}, {MockData: true}} {
		if _, err := s.createSingle(context.Background(), command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("legacy creation should fail before storage: %v", err)
		}
		batch := &importBatchState{tree: &createTreeState{service: s}}
		if _, err := batch.prepare(command); !IsImportValidationError(err) {
			t.Fatalf("legacy import should fail before storage: %v", err)
		}
	}
}
