package main

// 启动合同：只监听私有 socket；拒绝覆盖普通文件、符号链接和已运行实例。
import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/infra/sourceprotocol"
	"github.com/stretchr/testify/require"
)

func TestServeSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "r.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, socket) }()
	client := sourceprotocol.NewRemoteModbus(socket)
	defer client.Close()
	require.Eventually(t, func() bool { return client.Ready(context.Background()) == nil }, 5*time.Second, 10*time.Millisecond)
	info, err := os.Stat(socket)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.ErrorContains(t, serve(ctx, socket), "already running")
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown blocked")
	}
	require.ErrorIs(t, client.Ready(context.Background()), sourcestream.ErrUnavailable)
}

func TestServeSocketSafety(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.Error(t, serve(ctx, "relative.sock"))
	root := t.TempDir()
	file := filepath.Join(root, "protected")
	require.NoError(t, os.WriteFile(file, []byte("preserve"), 0600))
	require.ErrorContains(t, serve(ctx, file), "occupied")
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(file, link))
	require.ErrorContains(t, serve(ctx, link), "occupied")
	content, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "preserve", string(content))
	// A crashed process may leave a socket inode; an inactive socket is safe
	// to reclaim, unlike a live instance or a non-socket path.
	socket := filepath.Join(root, "stale.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	require.NoError(t, err)
	listener.SetUnlinkOnClose(false)
	require.NoError(t, listener.Close())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, socket) }()
	client := sourceprotocol.NewRemoteModbus(socket)
	defer client.Close()
	require.Eventually(t, func() bool { return client.Ready(context.Background()) == nil }, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown blocked")
	}
}
