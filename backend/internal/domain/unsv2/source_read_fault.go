package unsv2

import (
	"backend/internal/domain/sourcestream"
)

// 公共资源读取与持久运行复用服务端安全分类，不返回上游正文或连接信息。
func sourceReadFault(err error) *Fault {
	diagnostic := sourcestream.SourceReadDiagnostic(err)
	if diagnostic == nil {
		return nil
	}
	return &Fault{Code: diagnostic.Code, Message: diagnostic.Message, Repair: diagnostic.Repair, Path: diagnostic.Path, Reason: diagnostic.Reason}
}
