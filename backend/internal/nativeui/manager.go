package nativeui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ManagerSnapshot is the shared, read-only desktop presentation model. Neither
// desktop frontend needs credentials or an HTTP management listener.
type ManagerSnapshot struct {
	Progress
	Product     string          `json:"product"`
	Version     string          `json:"version"`
	Root        string          `json:"root"`
	URL         string          `json:"url"`
	Selected    ServiceProgress `json:"selected"`
	Description string          `json:"description"`
	Endpoint    string          `json:"endpoint"`
	Log         string          `json:"log"`
	LogFile     string          `json:"logFile"`
}

func StateLabel(state string) string {
	if label := map[string]string{"pending": "等待启动", "starting": "启动中", "running": "进程运行", "ready": "已就绪", "stopping": "停止中", "stopped": "已停止", "failed": "运行异常", "unknown": "状态未知"}[state]; label != "" {
		return label
	}
	return state
}

func Manager(root, program, product, selected string, p Progress) ManagerSnapshot {
	m := ManagerSnapshot{Progress: p, Product: product, Root: root, Version: "dev"}
	settings := map[string]string{}
	if b, e := os.ReadFile(filepath.Join(root, "state", "environment.json")); e == nil {
		_ = json.Unmarshal(b, &settings)
	}
	port := "18080"
	if n, e := strconv.Atoi(settings["NATIVE_HTTP_PORT"]); e == nil && n > 0 && n < 65536 {
		port = strconv.Itoa(n)
	}
	m.URL = "http://127.0.0.1:" + port
	var manifest struct {
		ProductVersion string `json:"productVersion"`
		PackageVersion string `json:"packageVersion"`
	}
	if b, e := os.ReadFile(filepath.Join(program, "package.json")); e == nil && json.Unmarshal(b, &manifest) == nil && manifest.ProductVersion != "" {
		m.Version = manifest.ProductVersion
		if manifest.PackageVersion != "" {
			m.Version = manifest.PackageVersion
		}
	}
	for _, s := range p.Services {
		if s.ID == selected {
			m.Selected = s
			break
		}
	}
	if m.Selected.ID == "" && len(p.Services) > 0 {
		m.Selected = p.Services[0]
	}
	id := m.Selected.ID
	if name := map[string]string{"backend": "Tier0 Core", "postgres": "PostgreSQL", "apps": "App Runtime"}[id]; name != "" {
		m.Selected.Label = name
	}
	m.Description = map[string]string{"backend": "平台接口与网页服务", "postgres": "业务数据库与时序数据存储", "nats": "内部消息与事件传递", "mqtt": "MQTT 协议接入", "opcua": "OPC UA 数据采集", "modbus": "Modbus 数据采集", "executor": "数据流执行服务", "fleet": "节点连接与运行管理", "apps": "Node.js 应用运行环境"}[id]
	m.Endpoint = map[string]string{"backend": port, "postgres": "15432", "nats": "14222", "mqtt": "11883", "fleet": "19732", "opcua": "本地 IPC", "modbus": "本地 IPC", "executor": "本地 IPC", "apps": "按应用分配"}[id]
	// Only names from the fixed service registry can resolve a log file.
	logID := map[string]string{"backend": "backend", "postgres": "postgres", "nats": "nats", "mqtt": "mqtt", "fleet": "fleet", "opcua": "opcua", "modbus": "modbus", "executor": "executor", "apps": "backend"}[id]
	if logID != "" {
		m.LogFile = filepath.Join(root, "logs", logID+".log")
		m.Log = logTail(m.LogFile, settings)
	}
	return m
}

var desktopCredential = regexp.MustCompile(`(?i)((?:password|secret|token|authorization|api[_-]?key)["']?\s*[:=]\s*["']?)([^\r\n,}]+)`)
var desktopURLCredential = regexp.MustCompile(`(\w+://)[^\s/@]+:[^\s/@]+@`)

func logTail(path string, settings map[string]string) string {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "暂无日志，服务启动后将在此显示。"
	}
	if err != nil {
		return "无法读取日志：" + err.Error()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "无法读取日志状态"
	}
	const limit int64 = 32 * 1024
	offset := max(int64(0), info.Size()-limit)
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return "无法定位最近日志"
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "无法读取最近日志"
	}
	lines := strings.Split(strings.ToValidUTF8(string(b), "�"), "\n")
	if offset > 0 && len(lines) > 1 {
		lines = lines[1:]
	}
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	for k, v := range settings {
		if len(v) >= 6 && (strings.Contains(k, "PASSWORD") || strings.Contains(k, "SECRET") || strings.Contains(k, "TOKEN") || strings.HasSuffix(k, "_KEY")) {
			text = strings.ReplaceAll(text, v, "[redacted]")
		}
	}
	text = desktopCredential.ReplaceAllString(text, "${1}[redacted]")
	text = desktopURLCredential.ReplaceAllString(text, "${1}[redacted]@")
	if text == "" {
		return "暂无日志，服务启动后将在此显示。"
	}
	return text
}

func (m ManagerSnapshot) Summary() string {
	n := 0
	for _, s := range m.Services {
		if s.State == "ready" || s.State == "running" {
			n++
		}
	}
	if m.Phase == "ready" {
		return fmt.Sprintf("%d / %d 服务运行中", n, len(m.Services))
	}
	return StateLabel(m.Phase)
}
