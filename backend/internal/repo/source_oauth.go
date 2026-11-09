package repo

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
)

// ChangeCredential serializes credential use/rotation across backend instances
// without creating a new Source configuration or invalidating deployed Streams.
func (r *SourceStreamRepo) ChangeCredential(ctx context.Context, id int64, change func(Source, *SourceCredential) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, data, err := sourceRow(tx, id, "UPDATE")
		if err != nil {
			return err
		}
		var connection struct {
			CredentialRef string `json:"credentialRef"`
		}
		if json.Unmarshal(data.Config.Connection, &connection) != nil {
			return ErrInvalidArgument
		}
		credential, ok := data.Credentials[connection.CredentialRef]
		if !ok {
			return ErrNotFound
		}
		source := Source{ID: id, Document: string(data.Config.Connection), ConfigRevision: data.ConfigRevision, Revision: row.Revision, DesiredState: data.DesiredState}
		if err = change(source, &credential); err != nil {
			return err
		}
		return NewSourceStreamRepo(tx).SaveCredential(ctx, credential)
	})
}
