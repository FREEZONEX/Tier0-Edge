package unsv2

import "strings"

// 兼容入口保留默认 before；统一入口的 none 映射为空值，继续沿用原写事务和权限判断。
func singleWriteReturnDocument(action, requested string) (string, error) {
	legacy := strings.HasPrefix(action, "findOneAnd")
	supported := contains([]string{"insertOne", "updateOne", "replaceOne", "deleteOne", "findOneAndUpdate", "findOneAndReplace", "findOneAndDelete"}, action)
	if !supported {
		if requested != "" {
			return "", invalid("returnDocument", singleWriteReturnUnsupportedMessage)
		}
		return "", nil
	}
	if requested == "" && legacy {
		requested = "before"
	}
	if !legacy && (requested == "" || requested == "none") {
		return "", nil
	}
	if requested != "before" && requested != "after" {
		return "", invalid("returnDocument", singleWriteReturnInvalidMessage)
	}
	if action == "insertOne" && requested != "after" {
		return "", invalid("returnDocument", singleWriteInsertReturnMessage)
	}
	if (action == "deleteOne" || action == "findOneAndDelete") && requested == "after" {
		return "", invalid("returnDocument", singleWriteDeleteReturnMessage)
	}
	return requested, nil
}

const (
	singleWriteReturnUnsupportedMessage = "returnDocument is not supported by this write operation"
	singleWriteReturnInvalidMessage     = "returnDocument must be none, before or after"
	singleWriteInsertReturnMessage      = "insert can only return the created document"
	singleWriteDeleteReturnMessage      = "delete can only return the previous document"
	singleWriteProjectionMessage        = "projection requires returnDocument before or after"
)
