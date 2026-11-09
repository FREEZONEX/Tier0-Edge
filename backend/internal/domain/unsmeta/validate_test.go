package unsmeta_test

// 校验和保存共用引用检查，校验不写库，并拒绝丢失引用和过期 revision。
import (
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

// 对齐 Cloud：格式只声明表示方式，不增加范围、精度或日期有效性约束。
func TestCloudSchemaFormatAnnotations(t *testing.T) {
	for _, tc := range []struct {
		kind, format string
		valid        bool
	}{
		{"integer", "", true}, {"number", "", true}, {"string", "", true},
		{"integer", unsmeta.NumericFormatInt32, true},
		{"number", unsmeta.NumericFormatFloat, true}, {"number", unsmeta.NumericFormatDouble, true},
		{"string", unsmeta.SchemaFormatDateTime, true},
		{"number", unsmeta.NumericFormatInt32, false}, {"integer", unsmeta.NumericFormatFloat, false},
		{"string", unsmeta.NumericFormatDouble, false}, {"number", unsmeta.SchemaFormatDateTime, false},
		{"integer", "int64", false}, {"number", "decimal", false}, {"string", "date", false},
	} {
		t.Run(tc.kind+"/"+tc.format, func(t *testing.T) {
			var schema unsmeta.Schema
			require.NoError(t, unsmeta.Parse(map[string]any{"type": tc.kind, "format": tc.format}, &schema))
			require.Equal(t, tc.valid, schema.Validate() == nil)
		})
	}
	for _, tc := range []struct {
		schema unsmeta.Schema
		value  string
	}{
		{unsmeta.Schema{Type: "integer", Format: unsmeta.NumericFormatInt32}, "2147483648"},
		{unsmeta.Schema{Type: "number", Format: unsmeta.NumericFormatFloat}, "1e100"},
		{unsmeta.Schema{Type: "number", Format: unsmeta.NumericFormatDouble}, "1e400"},
		{unsmeta.Schema{Type: "number", Format: unsmeta.NumericFormatFloat}, "0.1234567890123456789"},
	} {
		value := json.Number(tc.value)
		require.True(t, tc.schema.Accepts(value))
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, tc.value, string(raw))
		require.False(t, tc.schema.Accepts(tc.value))
	}
	schema := unsmeta.Schema{Type: "string", Format: unsmeta.SchemaFormatDateTime}
	for _, value := range []string{"2026-10-26T10:00:00+08:00", "2026-02-30", "not a date", ""} {
		require.True(t, schema.Accepts(value))
	}
	require.False(t, schema.Accepts(123))
}

func TestValidateTopicNoWrites(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	s := unsmeta.New()
	folder, err := s.Save(ctx, "", "folder", 0, unsmeta.Document{Name: "line"})
	require.NoError(t, err)
	doc := unsmeta.Document{Name: "temperature", ParentID: folder.ID}
	before, err := s.List(ctx, "", false, 1, 100)
	require.NoError(t, err)
	require.NoError(t, s.ValidateTopic(ctx, "", 0, doc))
	after, err := s.List(ctx, "", false, 1, 100)
	require.NoError(t, err)
	require.Equal(t, before, after)
	topic, err := s.Save(ctx, "", "topic", 0, doc)
	require.NoError(t, err)
	require.NoError(t, s.ValidateTopic(ctx, topic.ID, topic.Revision, doc))
	require.ErrorIs(t, s.ValidateTopic(ctx, topic.ID, topic.Revision+1, doc), repo.ErrConflict)
	doc.ParentID = topic.ID
	require.Error(t, s.ValidateTopic(ctx, "", 0, doc))
	doc.ParentID = "00000000-0000-0000-0000-000000000009"
	require.Error(t, s.ValidateTopic(ctx, "", 0, doc))
	doc.ParentID = ""
	doc.CollectionID = folder.ID
	require.Error(t, s.ValidateTopic(ctx, "", 0, doc))
	doc.CollectionID = ""
	doc.Fields = []unsmeta.FieldBinding{{Name: "x", Kind: "record", CollectionID: folder.ID, RecordID: "test", FieldPath: "x"}}
	require.Error(t, s.ValidateTopic(ctx, "", 0, doc))
	require.Error(t, s.ValidateTopic(ctx, "invalid", 1, unsmeta.Document{Name: "test"}))
	require.Error(t, s.ValidateTopic(ctx, "", 1, unsmeta.Document{Name: "test"}))
	require.Error(t, s.ValidateTopic(ctx, "", 0, map[string]any{"name": "test", "unknown": true}))
}

// 已有记录时只允许接受原有全部值的 Schema 扩展，不能只看当前样本是否碰巧兼容。
func TestCloudCollectionSchemaWidening(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	s := unsmeta.New()
	closed := false
	minimum, maximum, lower, higher := float64(0), float64(10), float64(-1), float64(11)
	for _, tc := range []struct {
		name          string
		before, after unsmeta.Schema
		allowed       bool
	}{
		{"number", unsmeta.Schema{Type: "integer"}, unsmeta.Schema{Type: "number"}, true},
		{"enum-expand", unsmeta.Schema{Type: "integer", Enum: []any{1, 2}}, unsmeta.Schema{Type: "integer", Enum: []any{1, 2, 3}}, true},
		{"enum-narrow", unsmeta.Schema{Type: "integer", Enum: []any{1, 2}}, unsmeta.Schema{Type: "integer", Enum: []any{1}}, false},
		{"bounds-expand", unsmeta.Schema{Type: "number", Minimum: &minimum, Maximum: &maximum}, unsmeta.Schema{Type: "number", Minimum: &lower, Maximum: &higher}, true},
		{"bounds-add", unsmeta.Schema{Type: "number"}, unsmeta.Schema{Type: "number", Minimum: &minimum}, false},
		{"annotation", unsmeta.Schema{Type: "integer", Title: "Before"}, unsmeta.Schema{Type: "integer", Title: "After"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := unsmeta.Document{Name: tc.name, RecordSchema: &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": tc.before}, Required: []string{"value"}, AdditionalProperties: &closed}}
			row, err := s.Save(ctx, "", "collection", 0, doc)
			require.NoError(t, err)
			_, err = s.SaveRecord(ctx, row.ID, "retained", 0, map[string]any{"value": 1})
			require.NoError(t, err)
			doc.RecordSchema.Properties["value"] = tc.after
			doc.RecordSchema.Required = nil
			_, err = s.Save(ctx, row.ID, "collection", row.Revision, doc)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, unsmeta.ErrSchemaInUse)
			}
		})
	}
}
