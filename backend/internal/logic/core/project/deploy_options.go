package project

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"backend/internal/service/tenantpg"
	"backend/internal/types"
)

// platformForcedEnvs 平台自动注入且不允许被用户/包覆盖的变量。
// 与 generateComposeYAML 的强制集合保持一致，用于导入时拒绝用户提交强制变量。
var platformForcedEnvs = map[string]bool{
	"DATABASE_URL":     true,
	"DB_SCHEMA":        true,
	"PORT":             true,
	"HOSTNAME":         true,
	"APP_ID":           true,
	"TIER0_PROJECT_ID": true,
	"TIER0_API_KEY":    true,
	"TIER0_API_HOST":   true,
	"TIER0_MQTT_HOST":  true,
	"TIER0_MQTT_PORT":  true,
}

// buildEditableEnvVarItems 将包内环境变量转换为导入预览项，并过滤平台强制变量。
// 这些变量由部署平台统一注入，不能作为用户可编辑项回传后再以 replace 策略提交。
func buildEditableEnvVarItems(envs map[string]string) []types.EnvVarItem {
	items := make([]types.EnvVarItem, 0, len(envs))
	for key, value := range envs {
		if platformForcedEnvs[key] {
			continue
		}
		items = append(items, types.EnvVarItem{Key: key, Value: value})
	}
	return items
}

// artifactDeployOptions 部署期可选项，零值保持现状行为（schema 用 app_{自增ID}、env 用包内值）。
type artifactDeployOptions struct {
	networkAlias string
	// schemaName 显式指定的应用 schema 名；空则回退 app_{appRecordID}
	schemaName string
	// envPolicy "inherit"（默认）用包内 artifact env；"replace" 用 envVars 作为最终列表
	envPolicy string
	// envVars 用户提交的最终环境变量列表（envPolicy=replace 时生效）
	envVars map[string]string
	// volumePolicy "inherit"（默认）沿用 manifest 已持久化 volumes；"replace" 用 volumes 作为最终列表
	volumePolicy string
	// volumes 最终生效的磁盘映射列表；调用部署前由 logic 层归一为最终列表（部署层只看该字段）
	volumes []artifactVolume
}

// artifactVolume 用户自定义磁盘映射：宿主机路径 bind mount 到容器路径，JSON 形态与 manifest 持久化一致
type artifactVolume struct {
	HostPath      string `json:"hostPath"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly"`
}

// envVarKeyRe 环境变量 key 格式：字母/下划线开头，后跟字母数字下划线
var envVarKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	maxEnvVarCount     = 200
	maxEnvVarKeyLen    = 128
	maxEnvVarValueLen  = 4096
	maxEnvVarTotalSize = 256 * 1024

	maxVolumeCount    = 50
	maxVolumePathLen  = 4096
	volumeManifestKey = "volumes"
)

// reservedContainerPrefixes 容器内平台保留路径（等值或子路径均拒绝映射）
var reservedContainerPrefixes = []string{"/app/data", "/proc", "/sys", "/dev"}

// normalizeEnvVars 校验并转换用户提交的环境变量列表为 map。
// 规则：拒绝重复 key、非法 key 格式、数量/长度/总大小超限；提交平台强制变量直接拒绝（不静默忽略）。
func normalizeEnvVars(envVars []types.EnvVarItem) (map[string]string, error) {
	if len(envVars) == 0 {
		return map[string]string{}, nil
	}
	if len(envVars) > maxEnvVarCount {
		return nil, fmt.Errorf("环境变量数量超过上限（%d）", maxEnvVarCount)
	}
	out := make(map[string]string, len(envVars))
	total := 0
	for _, item := range envVars {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			return nil, fmt.Errorf("环境变量 key 不能为空")
		}
		if !envVarKeyRe.MatchString(key) {
			return nil, fmt.Errorf("环境变量 key 格式非法: %s", key)
		}
		if len(key) > maxEnvVarKeyLen {
			return nil, fmt.Errorf("环境变量 key 超过长度上限: %s", key)
		}
		if platformForcedEnvs[key] {
			return nil, fmt.Errorf("环境变量 %s 为平台保留变量，不允许修改", key)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("环境变量 key 重复: %s", key)
		}
		value := item.Value
		if len(value) > maxEnvVarValueLen {
			return nil, fmt.Errorf("环境变量 %s 的值超过长度上限", key)
		}
		total += len(key) + len(value)
		if total > maxEnvVarTotalSize {
			return nil, fmt.Errorf("环境变量总大小超过上限")
		}
		out[key] = value
	}
	return out, nil
}

// resolveArtifactEnv 根据 envPolicy 解析最终生效的 artifact env：
// "replace" 用用户提交列表（已校验）；其余（inherit/空）用包内 env。
// 返回值只包含非平台强制变量；平台强制变量由 generateComposeYAML 强制写入。
func resolveArtifactEnv(pkgEnv map[string]string, policy string, userEnv map[string]string) map[string]string {
	if policy == "replace" {
		return userEnv
	}
	return pkgEnv
}

// normalizeVolumePath 校验单个挂载路径：非空、绝对路径、Clean 后不变（即不含 ..、.、多余斜杠等非规范形态），
// 并拒绝冒号/换行/双引号（防 compose YAML 破坏或挂载语法注入）。
func normalizeVolumePath(p, label string) (string, error) {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return "", fmt.Errorf("磁盘映射的%s不能为空", label)
	}
	if len(trimmed) > maxVolumePathLen {
		return "", fmt.Errorf("磁盘映射的%s超过长度上限（%d）: %s", label, maxVolumePathLen, trimmed)
	}
	if strings.ContainsAny(trimmed, ":\n\"") {
		return "", fmt.Errorf("磁盘映射的%s不允许包含冒号、换行或双引号: %s", label, trimmed)
	}
	cleaned := path.Clean(trimmed)
	if !path.IsAbs(cleaned) {
		return "", fmt.Errorf("磁盘映射的%s必须为绝对路径: %s", label, trimmed)
	}
	if cleaned != trimmed || strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("磁盘映射的%s必须为规范绝对路径（不允许 .. 或冗余路径段）: %s", label, trimmed)
	}
	return cleaned, nil
}

// isReservedContainerPath 容器路径命中根目录或平台保留前缀（等值或子路径）时拒绝映射
func isReservedContainerPath(p string) bool {
	if p == "/" {
		return true
	}
	for _, reserved := range reservedContainerPrefixes {
		if p == reserved || strings.HasPrefix(p, reserved+"/") {
			return true
		}
	}
	return false
}

// normalizeVolumes 校验并转换用户提交的磁盘映射列表。
// 规则：hostPath/containerPath 均为非空规范绝对路径且不含非法字符；hostPath 拒绝根目录；
// 容器路径拒绝平台保留路径（/、/app/data、/proc、/sys、/dev 及其子路径）；按 containerPath 判重；数量超限直接拒绝。
func normalizeVolumes(volumes []types.AppVolumeItem) ([]artifactVolume, error) {
	if len(volumes) == 0 {
		return nil, nil
	}
	if len(volumes) > maxVolumeCount {
		return nil, fmt.Errorf("磁盘映射数量超过上限（%d）", maxVolumeCount)
	}
	out := make([]artifactVolume, 0, len(volumes))
	seen := make(map[string]bool, len(volumes))
	for _, item := range volumes {
		hostPath, err := normalizeVolumePath(item.HostPath, "宿主机路径")
		if err != nil {
			return nil, err
		}
		if hostPath == "/" {
			return nil, fmt.Errorf("磁盘映射的宿主机路径不允许为根目录 /")
		}
		containerPath, err := normalizeVolumePath(item.ContainerPath, "容器路径")
		if err != nil {
			return nil, err
		}
		if isReservedContainerPath(containerPath) {
			return nil, fmt.Errorf("磁盘映射的容器路径 %s 为平台保留路径，不允许挂载", containerPath)
		}
		if seen[containerPath] {
			return nil, fmt.Errorf("磁盘映射的容器路径重复: %s", containerPath)
		}
		seen[containerPath] = true
		out = append(out, artifactVolume{HostPath: hostPath, ContainerPath: containerPath, ReadOnly: item.ReadOnly})
	}
	return out, nil
}

// resolveArtifactVolumes 根据 volumePolicy 解析最终生效的磁盘映射：
// "replace" 用用户提交列表（已校验）；其余（inherit/空）沿用 existingVolumes
// （Replace 场景为 manifest 已持久化列表，首次导入为空）。
func resolveArtifactVolumes(policy string, userVolumes, existingVolumes []artifactVolume) []artifactVolume {
	if policy == "replace" {
		return userVolumes
	}
	return existingVolumes
}

// manifestVolumes 从 manifest 读取已持久化的磁盘映射列表；缺省/格式异常返回空切片（不阻断调用方）
func manifestVolumes(manifest map[string]any) []artifactVolume {
	raw, ok := manifest[volumeManifestKey]
	if !ok || raw == nil {
		return []artifactVolume{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return []artifactVolume{}
	}
	var items []artifactVolume
	if err := json.Unmarshal(data, &items); err != nil || items == nil {
		return []artifactVolume{}
	}
	return items
}

// volumesOrEmpty 把 nil 归一为空切片，保证 manifest 持久化为 [] 而非 null
func volumesOrEmpty(volumes []artifactVolume) []artifactVolume {
	if volumes == nil {
		return []artifactVolume{}
	}
	return volumes
}

// buildImportOptions 从请求字段构造部署选项：校验 schema 名、用户环境变量与用户磁盘映射。
// schemaName 为空（Replace/未填高级区）时只校验 env/volumes；
// envPolicy/volumePolicy 非 "replace" 时忽略对应提交列表。
func buildImportOptions(schemaName, envPolicy string, envVars []types.EnvVarItem, volumePolicy string, volumes []types.AppVolumeItem, networkAlias ...string) (artifactDeployOptions, error) {
	opts := artifactDeployOptions{
		schemaName:   strings.TrimSpace(schemaName),
		envPolicy:    envPolicy,
		volumePolicy: volumePolicy,
	}
	if len(networkAlias) > 0 {
		opts.networkAlias = strings.TrimSpace(networkAlias[0])
		if err := validateAppNetworkAlias(opts.networkAlias); err != nil {
			return opts, err
		}
	}
	// 校验用户提交 schema：非空时必须是合法 PG 标识符且非系统保留名
	if opts.schemaName != "" {
		if isReservedSystemSchema(opts.schemaName) {
			return opts, fmt.Errorf("schema 名 %s 为系统保留名称，不允许使用", opts.schemaName)
		}
		if err := tenantpg.ValidateIdentifier(opts.schemaName); err != nil {
			return opts, fmt.Errorf("schema 名非法（仅允许小写字母/数字/下划线，长度≤63）: %s", opts.schemaName)
		}
	}
	if envPolicy == "replace" {
		env, err := normalizeEnvVars(envVars)
		if err != nil {
			return opts, err
		}
		opts.envVars = env
	}
	if volumePolicy == "replace" {
		parsedVolumes, err := normalizeVolumes(volumes)
		if err != nil {
			return opts, err
		}
		opts.volumes = parsedVolumes
	}
	return opts, nil
}
