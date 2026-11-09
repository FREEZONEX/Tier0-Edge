// Package appstatus 收敛应用状态的口径。
//
// 部署状态原来只在 project 域里算（deployStatusForList），launchpad 域要用同一套
// 口径却导不进来（launchpad → project → iam → launchpad 会成环），复制一份又会随
// 状态演进悄悄漂移。放到这个不依赖任何域的小包里，两边共用同一份实现。
package appstatus

import "strings"

// Normalize 把库里/接口传入的应用状态归一化;认不出来的返回空串。
func Normalize(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "1":
		return "active"
	case "disabled", "disable", "0":
		return "disabled"
	case "stopped", "stop":
		return "stopped"
	case "deploying":
		return "deploying"
	case "failed":
		return "failed"
	default:
		return ""
	}
}

// DeployStatus 列表口径的部署状态:success / deploying / failed / stopped / pending。
// deploying 与 deployError 来自应用 manifest,优先级高于库里的状态。
func DeployStatus(status string, deploying bool, deployError string) string {
	if deploying {
		return "deploying"
	}
	if deployError != "" {
		return "failed"
	}
	switch Normalize(status) {
	case "active":
		return "success"
	case "stopped":
		return "stopped"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}
