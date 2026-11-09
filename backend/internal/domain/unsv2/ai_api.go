package unsv2

import "context"

// Explicit entrypoints share the original authorization and execution engines.
func (s *Service) executeAI(ctx context.Context, operation string, raw []byte) (any, bool, error) {
	if BatchScope(operation) != "" {
		out, err := s.executeScopedBatch(ctx, operation, raw)
		return out, true, err
	}
	switch operation {
	case "dataDiscover":
		var input DiscoverInput
		if err := Decode(raw, &input); err != nil {
			return nil, true, err
		}
		out, err := s.Discover(ctx, input)
		return out, true, err
	case "collectionDescribe":
		var input DescribeCollectionInput
		if err := Decode(raw, &input); err != nil {
			return nil, true, err
		}
		out, err := s.DescribeCollection(ctx, input)
		return out, true, err
	case "recordSearch", "recordAggregate", "recordQueryValidate":
		var input RecordQueryRequest
		if err := Decode(raw, &input); err != nil {
			return nil, true, err
		}
		out, err := s.RecordQuery(ctx, operation, input)
		return out, true, err
	case "transactionValidate", "transactionExecute":
		var input Batch
		if err := Decode(raw, &input); err != nil {
			return nil, true, err
		}
		if operation == "transactionValidate" {
			out, err := s.ValidateTransaction(ctx, input)
			return out, true, err
		}
		if input.DryRun {
			return nil, true, invalid("dryRun", "Use batch/validate for previews; batch/execute commits changes")
		}
		out, err := s.Apply(ctx, input)
		return out, true, err
	case "transactionStatus":
		var input TransactionStatusInput
		if err := Decode(raw, &input); err != nil {
			return nil, true, err
		}
		out, err := s.TransactionStatus(ctx, input)
		return out, true, err
	}
	return nil, false, nil
}
