package sourcestream

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPBoundedListenerShares64SocketBudgetAndReleasesExactlyOnce(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	budget := make(chan struct{}, 64)
	one := &httpBoundedListener{Listener: first, slots: budget}
	two := &httpBoundedListener{Listener: second, slots: budget}
	var clients, accepted []net.Conn
	t.Cleanup(func() {
		for _, conn := range accepted {
			_ = conn.Close()
		}
		for _, conn := range clients {
			_ = conn.Close()
		}
	})
	for i := 0; i < cap(budget); i++ {
		client, err := net.DialTimeout("tcp", first.Addr().String(), time.Second)
		require.NoError(t, err)
		clients = append(clients, client)
		conn, err := one.Accept()
		require.NoError(t, err)
		accepted = append(accepted, conn)
	}
	require.Len(t, budget, 64)
	type acceptResult struct {
		conn net.Conn
		err  error
	}
	results := make(chan acceptResult, 1)
	go func() { conn, err := two.Accept(); results <- acceptResult{conn, err} }()
	overflow, err := net.DialTimeout("tcp", second.Addr().String(), time.Second)
	require.NoError(t, err)
	clients = append(clients, overflow)
	require.NoError(t, overflow.SetReadDeadline(time.Now().Add(time.Second)))
	line, err := bufio.NewReader(overflow).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "HTTP/1.1 503 Service Unavailable", strings.TrimSpace(line))
	require.Len(t, budget, 64, "another Source listener cannot bypass the shared budget")
	var closes sync.WaitGroup
	for i := 0; i < 16; i++ {
		closes.Add(1)
		go func() { defer closes.Done(); _ = accepted[0].Close() }()
	}
	closes.Wait()
	require.Len(t, budget, 63, "concurrent repeated Close releases exactly one slot")
	next, err := net.DialTimeout("tcp", second.Addr().String(), time.Second)
	require.NoError(t, err)
	clients = append(clients, next)
	select {
	case result := <-results:
		require.NoError(t, result.err)
		require.NotNil(t, result.conn)
		accepted = append(accepted, result.conn)
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not recover after a slot was released")
	}
	require.Len(t, budget, 64)
	go func() { conn, err := two.Accept(); results <- acceptResult{conn, err} }()
	require.NoError(t, two.Close())
	select {
	case result := <-results:
		require.ErrorIs(t, result.err, net.ErrClosed)
		require.Nil(t, result.conn)
	case <-time.After(2 * time.Second):
		t.Fatal("Listener Close did not unblock Accept")
	}
	for _, conn := range accepted {
		_ = conn.Close()
	}
	require.Empty(t, budget)
}
