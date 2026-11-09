package fleettunnel

// 使用真实本机 WebSocket 验证字节流、截止时间与并发写入，不使用生产连接。
import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func websocketPair(t *testing.T, activity func()) (*websocket.Conn, *websocketConn) {
	t.Helper()
	type upgradeResult struct {
		connection *websocket.Conn
		err        error
	}
	accepted := make(chan upgradeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		accepted <- upgradeResult{connection, err}
	}))
	client, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	var result upgradeResult
	select {
	case result = <-accepted:
	case <-time.After(time.Second):
		_ = client.Close()
		server.Close()
		t.Fatal("fixture upgrade did not complete")
	}
	require.NoError(t, result.err)
	connection := newWebsocketConn(result.connection, activity)
	t.Cleanup(func() { _ = connection.Close(); _ = client.Close(); server.Close() })
	return client, connection
}

func TestWebsocketConnContinuousBinaryStream(t *testing.T) {
	var activity atomic.Int32
	client, connection := websocketPair(t, func() { activity.Add(1) })
	require.NotNil(t, connection.LocalAddr())
	require.NotNil(t, connection.RemoteAddr())
	require.NoError(t, connection.SetDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte("not stream data")))
	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, nil))
	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, []byte("ab")))
	require.NoError(t, client.WriteMessage(websocket.BinaryMessage, []byte("cdef")))
	data := make([]byte, 6)
	_, err := io.ReadFull(connection, data)
	require.NoError(t, err)
	require.Equal(t, "abcdef", string(data))
	require.GreaterOrEqual(t, activity.Load(), int32(2))
	n, err := connection.Write([]byte("reply"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	kind, received, err := client.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, kind)
	require.Equal(t, "reply", string(received))
	require.NoError(t, connection.Close())
	require.NoError(t, connection.Close())
}

func TestWebsocketConnReadDeadline(t *testing.T) {
	_, connection := websocketPair(t, nil)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, err := connection.Read(make([]byte, 1))
	var timeout net.Error
	require.True(t, errors.As(err, &timeout))
	require.True(t, timeout.Timeout())
}

func TestWebsocketConnConcurrentWriteAndDeadline(t *testing.T) {
	client, connection := websocketPair(t, nil)
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	start, finish, writerDone, setterDone := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan error, 1)
	var once sync.Once
	stop := func() { once.Do(func() { close(finish) }) }
	t.Cleanup(stop)
	go func() {
		<-start
		for index := 0; index < 128; index++ {
			if _, err := connection.Write([]byte{byte(index)}); err != nil {
				writerDone <- err
				stop()
				return
			}
		}
		writerDone <- nil
		stop()
	}()
	go func() {
		<-start
		for {
			select {
			case <-finish:
				setterDone <- nil
				return
			default:
				if err := connection.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					setterDone <- err
					return
				}
			}
		}
	}()
	close(start)
	for index := 0; index < 128; index++ {
		kind, data, err := client.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, websocket.BinaryMessage, kind)
		require.Equal(t, []byte{byte(index)}, data)
	}
	require.NoError(t, <-writerDone)
	require.NoError(t, <-setterDone)
}
