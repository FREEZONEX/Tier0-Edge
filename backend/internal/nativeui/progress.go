package nativeui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ServiceProgress struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}
type Progress struct {
	Phase     string            `json:"phase"`
	Message   string            `json:"message"`
	Error     string            `json:"error,omitempty"`
	Services  []ServiceProgress `json:"services"`
	PID       int               `json:"pid"`
	UpdatedAt int64             `json:"updatedAt"`
}

func NewProgress() Progress {
	return Progress{Phase: "starting", PID: os.Getpid(), Services: []ServiceProgress{
		{ID: "postgres", Label: "PostgreSQL / TimescaleDB", State: "pending"}, {ID: "nats", Label: "NATS", State: "pending"}, {ID: "mqtt", Label: "MQTT", State: "pending"}, {ID: "opcua", Label: "OPC UA", State: "pending"}, {ID: "modbus", Label: "Modbus", State: "pending"}, {ID: "executor", Label: "Stream Executor", State: "pending"}, {ID: "fleet", Label: "Fleet Agent", State: "pending"}, {ID: "backend", Label: "Backend / Web", State: "pending"}, {ID: "apps", Label: "Node.js App 运行环境", State: "pending"}}}
}
func (p *Progress) Set(id, state, message string) {
	for i := range p.Services {
		if p.Services[i].ID == id {
			p.Services[i].State = state
			p.Services[i].Message = message
			return
		}
	}
}
func WriteProgress(root string, p Progress) error {
	p.UpdatedAt = time.Now().UnixMilli()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	name := filepath.Join(root, "state", "startup-progress.json")
	if err = os.WriteFile(name+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(name+".tmp", name)
}
func ReadProgress(root string) (Progress, error) {
	var p Progress
	raw, err := os.ReadFile(filepath.Join(root, "state", "startup-progress.json"))
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}
func (p Progress) Lines() string {
	lines := []string{}
	for _, s := range p.Services {
		state := map[string]string{"pending": "等待", "starting": "启动中", "running": "进程已启动", "ready": "已就绪", "stopped": "已停止", "failed": "失败"}[s.State]
		if state == "" {
			state = s.State
		}
		line := s.Label + "  ·  " + state
		if s.Message != "" {
			line += "  " + s.Message
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\r\n")
}
