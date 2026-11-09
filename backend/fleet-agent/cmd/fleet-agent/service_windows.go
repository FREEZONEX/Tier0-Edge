//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"

	"golang.org/x/sys/windows/svc"
)

const defaultWindowsServiceName = "Tier0FleetAgent"

type windowsServiceHandler struct {
	logger *slog.Logger
}

func runAsSystemService(logger *slog.Logger) (bool, error) {
	managed, err := svc.IsWindowsService()
	if err != nil {
		return false, err
	}
	if !managed {
		return false, nil
	}
	serviceName := os.Getenv("FLEET_AGENT_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultWindowsServiceName
	}
	return true, svc.Run(serviceName, &windowsServiceHandler{logger: logger})
}

func (h *windowsServiceHandler) Execute(
	_ []string,
	requests <-chan svc.ChangeRequest,
	statuses chan<- svc.Status,
) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(h.logger, ctx)
	}()
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					h.logger.Error("fleet agent Windows service stopped with error", "error", err)
					return false, 1
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil {
				h.logger.Error("fleet agent Windows service exited unexpectedly", "error", err)
				return false, 1
			}
			return false, 0
		}
	}
}
