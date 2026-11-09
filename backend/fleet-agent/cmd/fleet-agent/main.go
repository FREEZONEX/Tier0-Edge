package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend/fleet-agent/internal/center"
	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/connection"
	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/fleet-agent/internal/ha"
	"backend/fleet-agent/internal/hostinfo"
	"backend/fleet-agent/internal/httpserver"
	"backend/fleet-agent/internal/managedruntime"
	"backend/fleet-agent/internal/state"
	"backend/fleet-agent/internal/status"
	"backend/internal/fleetprotocol"
	"backend/internal/runtimeenv"
)

const (
	shutdownTimeout = 10 * time.Second
)

var version = "dev"

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "configure" {
		if err := runConfigure(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Fleet Agent configure failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("tier0-fleet-agent %s\n", version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	managed, err := runAsSystemService(logger)
	if err != nil {
		logger.Error("fleet agent service host stopped with error", "error", err)
		os.Exit(1)
	}
	if managed {
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(logger, ctx); err != nil {
		logger.Error("fleet agent stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, ctx context.Context) error {
	dataDir, err := config.ResolveDataDir("")
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	store, err := state.Open(context.Background(), dataDir)
	if err != nil {
		return fmt.Errorf("open local state: %w", err)
	}
	defer store.Close()
	cfg, err := config.Load(context.Background(), store)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.HA.Enabled {
		memberID, identityErr := store.EnsureHAMemberID(context.Background(), cfg.HA.MemberID)
		if identityErr != nil {
			return fmt.Errorf("load Fleet HA member identity: %w", identityErr)
		}
		cfg.HA.MemberID = memberID
	}

	startedAt := time.Now().UTC()
	statusService := status.New(version, fleetprotocol.ProtocolVersion, startedAt, cfg, hostinfo.Collect())
	containerService := fleetcontainer.New(cfg.InstallationID, store)
	defer containerService.Close()
	centerClient := center.New(cfg, version, statusService, containerService, logger)
	connectionService := connection.New(cfg, store, centerClient, statusService)
	var haController *ha.Controller
	var syncManager *ha.SyncManager
	if cfg.HA.Enabled {
		syncGate := ha.SyncGate(ha.NoopSyncGate{})
		if cfg.HA.Sync.Enabled {
			syncManager, err = ha.NewSyncManager(
				cfg,
				store,
				containerService,
				logger,
			)
			if err != nil {
				return fmt.Errorf("initialize Fleet HA rsync synchronization: %w", err)
			}
			syncGate = syncManager
		}
		haController, err = ha.NewController(
			cfg,
			store,
			containerService,
			centerClient,
			syncGate,
			ha.NetworkVIPChecker{},
			logger,
		)
		if err != nil {
			return fmt.Errorf("initialize Fleet HA controller: %w", err)
		}
	}
	server := httpserver.New(
		cfg.HTTPAddr,
		cfg.LocalTokenHash,
		statusService.Snapshot,
		containerService,
		connectionService,
		haController,
		syncManager,
		logger,
	)

	server.BindNetwork(centerClient)
	server.BindRuntime(managedruntime.New(&managedruntime.Docker{Containers: containerService}))
	errCh := make(chan error, 1)
	go func() {
		logger.Info(
			"fleet agent HTTP server starting",
			"address", cfg.HTTPAddr,
			"configured", cfg.CenterConfigured(),
			"version", version,
			"protocolVersion", fleetprotocol.ProtocolVersion,
		)
		errCh <- server.ListenAndServe()
	}()
	go centerClient.Run(ctx)
	go centerClient.RunNetwork(ctx)
	if haController != nil {
		go haController.Run(ctx)
	}
	if syncManager != nil {
		go syncManager.Run(ctx, func() bool {
			return haController.Status().Role == ha.RoleActive
		})
	}

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("serve local HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("fleet agent shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown local HTTP: %w", err)
	}

	logger.Info("fleet agent stopped")
	return nil
}
