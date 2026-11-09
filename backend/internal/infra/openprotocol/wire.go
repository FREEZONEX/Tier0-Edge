// Package openprotocol implements the read-only Rev 1 tightening-result subset.
// It uses the Go TCP/TLS-independent network stack on every supported platform.
package openprotocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

var ErrFrame = errors.New("OpenProtocol.invalidFrame")

type Frame struct {
	MID  int
	Data string
}

// Encode deliberately excludes recipe, job, tool and other write operations.
func Encode(mid int, data string) ([]byte, error) {
	switch mid {
	case 1, 3, 60, 62, 63, 9999:
		if data != "" {
			return nil, ErrFrame
		}
	case 64:
		if len(data) != 10 || !digits(data) {
			return nil, ErrFrame
		}
	default:
		return nil, ErrFrame
	}
	return []byte(fmt.Sprintf("%04d%04d0010        %s\x00", 20+len(data), mid, data)), nil
}

func Read(r *bufio.Reader) (Frame, error) {
	var f Frame
	header := make([]byte, 20)
	if _, err := io.ReadFull(r, header); err != nil {
		return f, err
	}
	if !digits(string(header[:8])) {
		return f, ErrFrame
	}
	n, _ := strconv.Atoi(string(header[:4]))
	if n < 20 || n > 1024 {
		return f, ErrFrame
	}
	rev := strings.TrimSpace(string(header[8:11]))
	if rev != "" && rev != "000" && rev != "001" {
		return f, ErrFrame
	}
	if header[11] != '0' && header[11] != '1' && header[11] != ' ' {
		return f, ErrFrame
	}
	for _, part := range []string{string(header[12:14]), string(header[14:16])} {
		if part != "  " && part != "00" && part != "01" {
			return f, ErrFrame
		}
	}
	for _, c := range header[16:] {
		if c != ' ' && c != '0' {
			return f, ErrFrame
		}
	}
	tail := make([]byte, n-20+1)
	if _, err := io.ReadFull(r, tail); err != nil {
		return f, err
	}
	if tail[len(tail)-1] != 0 {
		return f, ErrFrame
	}
	for _, b := range append(header, tail[:len(tail)-1]...) {
		if b < 32 || b > 126 {
			return f, ErrFrame
		}
	}
	f.MID, _ = strconv.Atoi(string(header[4:8]))
	f.Data = string(tail[:len(tail)-1])
	return f, nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type Controller struct {
	CellID    int    `json:"cellId"`
	ChannelID int    `json:"channelId"`
	Name      string `json:"controllerName"`
}

type Result struct {
	Controller
	TighteningID     int64   `json:"tighteningId"`
	VIN              string  `json:"vin"`
	Job              *int    `json:"job,omitempty"`
	Pset             int     `json:"pset"`
	BatchSize        *int    `json:"batchSize,omitempty"`
	BatchCounter     int     `json:"batchCounter"`
	TighteningStatus int     `json:"tighteningStatus"`
	TorqueStatus     int     `json:"torqueStatus"`
	AngleStatus      int     `json:"angleStatus"`
	TorqueRaw        int     `json:"torqueRaw"`
	TorqueScale      float64 `json:"torqueScale"`
	Torque           float64 `json:"torque"`
	TorqueMinRaw     *int    `json:"torqueMinRaw,omitempty"`
	TorqueMaxRaw     *int    `json:"torqueMaxRaw,omitempty"`
	TorqueTargetRaw  *int    `json:"torqueTargetRaw,omitempty"`
	Angle            int     `json:"angle"`
	AngleMin         *int    `json:"angleMin,omitempty"`
	AngleMax         *int    `json:"angleMax,omitempty"`
	AngleTarget      *int    `json:"angleTarget,omitempty"`
	SourceTimestamp  string  `json:"sourceTimestamp"`
	Timestamp        string  `json:"timestamp"`
	PsetModified     string  `json:"psetModified,omitempty"`
	BatchStatus      int     `json:"batchStatus"`
	History          bool    `json:"history"`
}

func fields(data string, sizes []int) ([]string, error) {
	out := make([]string, 0, len(sizes))
	pos := 0
	for i, n := range sizes {
		if pos+2+n > len(data) || data[pos:pos+2] != fmt.Sprintf("%02d", i+1) {
			return nil, ErrFrame
		}
		out = append(out, data[pos+2:pos+2+n])
		pos += 2 + n
	}
	if pos != len(data) {
		return nil, ErrFrame
	}
	return out, nil
}

func ParseController(f Frame) (Controller, error) {
	c := Controller{}
	if f.MID != 2 {
		return c, ErrFrame
	}
	v, err := fields(f.Data, []int{4, 2, 25})
	if err != nil || !digits(v[0]) || !digits(v[1]) {
		return c, ErrFrame
	}
	c.CellID, _ = strconv.Atoi(v[0])
	c.ChannelID, _ = strconv.Atoi(v[1])
	c.Name = strings.TrimRight(v[2], " ")
	if c.Name == "" {
		return c, ErrFrame
	}
	return c, nil
}

func ParseResult(f Frame, controller Controller) (Result, error) {
	r := Result{Controller: controller, TorqueScale: 0.01, History: f.MID == 65}
	var v []string
	var err error
	switch f.MID {
	case 61:
		v, err = fields(f.Data, []int{4, 2, 25, 25, 2, 3, 4, 4, 1, 1, 1, 6, 6, 6, 6, 5, 5, 5, 5, 19, 19, 1, 10})
	case 65:
		v, err = fields(f.Data, []int{10, 25, 3, 4, 1, 1, 1, 6, 5, 19, 1})
	default:
		return r, ErrFrame
	}
	if err != nil {
		return r, err
	}
	number := func(index int) int {
		if !digits(v[index]) {
			err = ErrFrame
			return 0
		}
		n, e := strconv.Atoi(v[index])
		if e != nil {
			err = ErrFrame
		}
		return n
	}
	pointer := func(index int) *int { n := number(index); return &n }
	if f.MID == 61 {
		r.CellID = number(0)
		r.ChannelID = number(1)
		r.Name = strings.TrimRight(v[2], " ")
		if r.Controller != controller {
			return r, ErrFrame
		}
		r.VIN = strings.TrimRight(v[3], " ")
		r.Job = pointer(4)
		r.Pset = number(5)
		r.BatchSize = pointer(6)
		r.BatchCounter = number(7)
		r.TighteningStatus = number(8)
		r.TorqueStatus = number(9)
		r.AngleStatus = number(10)
		r.TorqueMinRaw = pointer(11)
		r.TorqueMaxRaw = pointer(12)
		r.TorqueTargetRaw = pointer(13)
		r.TorqueRaw = number(14)
		r.AngleMin = pointer(15)
		r.AngleMax = pointer(16)
		r.AngleTarget = pointer(17)
		r.Angle = number(18)
		r.SourceTimestamp = v[19]
		r.PsetModified = v[20]
		r.BatchStatus = number(21)
		if !digits(v[22]) {
			return r, ErrFrame
		}
		var idErr error
		r.TighteningID, idErr = strconv.ParseInt(v[22], 10, 64)
		if idErr != nil {
			err = idErr
		}
	} else {
		if !digits(v[0]) {
			return r, ErrFrame
		}
		r.TighteningID, err = strconv.ParseInt(v[0], 10, 64)
		r.VIN = strings.TrimRight(v[1], " ")
		r.Pset = number(2)
		r.BatchCounter = number(3)
		r.TighteningStatus = number(4)
		r.TorqueStatus = number(5)
		r.AngleStatus = number(6)
		r.TorqueRaw = number(7)
		r.Angle = number(8)
		r.SourceTimestamp = v[9]
		r.BatchStatus = number(10)
	}
	if err != nil || r.TighteningID <= 0 || r.TighteningStatus > 1 || r.TorqueStatus > 2 || r.AngleStatus > 2 || r.BatchStatus > 2 {
		return r, ErrFrame
	}
	stamp, err := time.ParseInLocation("2006-01-02:15:04:05", r.SourceTimestamp, time.FixedZone("Asia/Shanghai", 8*3600))
	if err != nil {
		return r, ErrFrame
	}
	if r.PsetModified != "" {
		if _, err = time.ParseInLocation("2006-01-02:15:04:05", r.PsetModified, time.FixedZone("Asia/Shanghai", 8*3600)); err != nil {
			return r, ErrFrame
		}
	}
	r.Timestamp = stamp.UTC().Format(time.RFC3339)
	r.Torque = float64(r.TorqueRaw) / 100
	return r, nil
}

type CommandError struct{ MID, Code int }

func (e *CommandError) Error() string {
	return fmt.Sprintf("OpenProtocol.commandRejected.%04d.%02d", e.MID, e.Code)
}
func CommandReply(f Frame, request int) error {
	if f.MID == 5 && len(f.Data) == 4 && f.Data == fmt.Sprintf("%04d", request) {
		return nil
	}
	if f.MID == 4 && len(f.Data) == 6 && digits(f.Data) {
		mid, _ := strconv.Atoi(f.Data[:4])
		code, _ := strconv.Atoi(f.Data[4:])
		if mid == request {
			return &CommandError{mid, code}
		}
	}
	return ErrFrame
}
