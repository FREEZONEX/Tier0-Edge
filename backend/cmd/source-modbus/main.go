// source-modbus owns shared, Source-isolated protocol connections. No DB or
// script initialization is performed in this process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"backend/internal/infra/localipc"
	"backend/internal/infra/sourceprotocol"
	"backend/internal/runtimeenv"
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	socket := flag.String("socket", "/run/source-modbus/runtime.sock", "private Unix socket")
	health := flag.Bool("health", false, "check private runtime readiness")
	flag.Parse()
	if *health {
		client := sourceprotocol.NewRemoteModbus(*socket)
		defer client.Close()
		if client.Ready(context.Background()) != nil {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, *socket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, socket string) error {
	if !filepath.IsAbs(socket) {
		return fmt.Errorf("absolute socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0750); err != nil {
		return err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path occupied")
		}
		if connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
			_ = connection.Close()
			return fmt.Errorf("Modbus runtime already running")
		} else if !localipc.StaleSocket(err) {
			return fmt.Errorf("cannot probe runtime socket")
		}
		if err := os.Remove(socket); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	runtime := sourceprotocol.NewModbusRuntime()
	defer runtime.Close()
	server := &http.Server{
		Handler: runtime, ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 * 1024,
		BaseContext:    func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		// Close transports as well as leases so an unacknowledged watch cannot
		// prevent a bounded shutdown. Device writes are never replayed.
		runtime.Close()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
