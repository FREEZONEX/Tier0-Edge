package unsmeta

// 格式与 Cloud 保持一致，仅声明表示方式，不改变基础类型、范围或精度校验。
const (
	NumericFormatInt32   = "int32"
	NumericFormatFloat   = "float"
	NumericFormatDouble  = "double"
	SchemaFormatDateTime = "date-time"
)

func validSchemaFormat(schemaType, format string) bool {
	switch format {
	case "":
		return true
	case NumericFormatInt32:
		return schemaType == "integer"
	case NumericFormatFloat, NumericFormatDouble:
		return schemaType == "number"
	case SchemaFormatDateTime:
		return schemaType == "string"
	default:
		return false
	}
}
