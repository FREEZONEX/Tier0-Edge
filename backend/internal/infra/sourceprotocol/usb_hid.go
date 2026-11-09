package sourceprotocol

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"github.com/google/uuid"
)

func hidCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	name := "tier0-hid-reader"
	switch runtime.GOOS {
	case "windows":
		name += ".exe"
	case "darwin":
	default:
		return nil, sourcestream.ErrUnavailable
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(executable), name), args...)
	configureHIDProcess(cmd)
	return cmd, nil
}

func listHIDDevices(ctx context.Context) ([]sourcestream.HIDDevice, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd, err := hidCommand(ctx, "--list")
	if err != nil {
		return nil, err
	}
	raw, err := cmd.Output()
	if err != nil {
		return nil, sourcestream.ErrUnavailable
	}
	candidates := map[string]sourcestream.HIDDevice{}
	counts := map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		legacy, product, ok := strings.Cut(strings.TrimSuffix(line, "\r"), "\t")
		if !ok {
			continue
		}
		if runtime.GOOS == "windows" {
			legacy = "usb-hid://windows/" + strings.ToUpper(strings.TrimPrefix(legacy, "usb-hid://windows/"))
		}
		def := sourcestream.SourceDefinition{Endpoint: legacy}
		if _, err = def.HIDDevice(); err != nil {
			continue
		}
		counts[legacy]++
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(legacy)))
		if strings.TrimSpace(product) == "" {
			product = "USB HID"
		}
		candidates[legacy] = sourcestream.HIDDevice{Endpoint: "usb-hid://device/" + digest, LegacyEndpoint: legacy, Label: fmt.Sprintf("%s · %s", product, digest[:8])}
	}
	devices := []sourcestream.HIDDevice{}
	for legacy, device := range candidates {
		// Composite USB devices can expose two collections with the same
		// native address. The reader deliberately refuses ambiguous matches.
		if counts[legacy] == 1 {
			devices = append(devices, device)
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Label < devices[j].Label })
	return devices, nil
}

func (r *Runtime) HIDDevices(ctx context.Context) ([]sourcestream.HIDDevice, error) {
	return listHIDDevices(ctx)
}

func (r *Runtime) PreviewHID(ctx context.Context, endpoint string, timeoutMs int) (sourcestream.HIDPreview, error) {
	var preview sourcestream.HIDPreview
	if !r.hidPreviewMu.TryLock() {
		return preview, repo.ErrConflict
	}
	defer r.hidPreviewMu.Unlock()
	life, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	ready := false
	observed := sourcestream.WithWatchObserver(life, func(up bool) {
		if up {
			ready = true
		}
	})
	err := watchHID(observed, sourcestream.SourceDefinition{Protocol: sourcestream.USBHID, Endpoint: endpoint}, func(input Input) error {
		data := input.Data.(map[string]any)
		preview = sourcestream.HIDPreview{Received: true, Text: data["text"].(string)}
		return context.Canceled
	})
	if preview.Received || (ready && errors.Is(life.Err(), context.DeadlineExceeded) && ctx.Err() == nil) {
		return preview, nil
	}
	return preview, err
}

func watchHID(ctx context.Context, def sourcestream.SourceDefinition, emit func(Input) error) error {
	if sourcestream.IsHIDDeviceKey(def.Endpoint) {
		devices, err := listHIDDevices(ctx)
		if err != nil {
			return err
		}
		for _, d := range devices {
			if d.Endpoint == def.Endpoint {
				def.Endpoint = d.LegacyEndpoint
				break
			}
		}
		if sourcestream.IsHIDDeviceKey(def.Endpoint) {
			return sourcestream.ErrUnavailable
		}
	}
	device, err := def.HIDDevice()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" && len(device) != 1 || runtime.GOOS == "darwin" && len(device) != 3 {
		return sourcestream.ErrUnavailable
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, err := hidCommand(ctx, device...)
	if err != nil {
		return err
	}
	status, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	ready := bufio.NewScanner(status)
	if !ready.Scan() || ready.Text() != "ready" {
		return errors.New("USB HID device unavailable or input monitoring permission denied")
	}
	if emit == nil {
		return nil // Probe only verifies non-exclusive access to the selected device.
	}
	sourcestream.ReportWatchReady(ctx, true)
	defer sourcestream.ReportWatchReady(ctx, false)
	lines := bufio.NewScanner(output)
	for lines.Scan() {
		now := time.Now().UnixMilli()
		data := map[string]any{"eventId": uuid.NewString(), "text": lines.Text(), "collectedAt": now}
		if err := emit(Input{ResourceRef: sourcestream.HIDTextResource, ReceivedAt: now, Data: data}); err != nil {
			return err
		}
	}
	if err := lines.Err(); err != nil {
		return err
	}
	return sourcestream.ErrUnavailable
}
