package mqtt

// 验证 Source 回调必须显式装配、不能取得超级用户权限或绕过保留 Topic ACL。
import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type sourceAccessFixture struct {
	allow bool
	err   error
}

func (f sourceAccessFixture) AuthenticateMQTTSource(context.Context, string, string, string) (bool, error) {
	return f.allow, f.err
}
func (f sourceAccessFixture) AuthorizeMQTTSource(context.Context, string, string, string, string) (bool, error) {
	return f.allow, f.err
}

func TestSourceAccessFailClosedAndNamespaceGuard(t *testing.T) {
	s := &Service{}
	ctx := context.Background()
	for _, allow := range []bool{false, true} {
		if allow {
			s.SetSourceAccess(sourceAccessFixture{allow: true})
		}
		auth, err := s.Auth(ctx, "source:42:credential:user", "secret", "device", "")
		require.NoError(t, err)
		require.Equal(t, allow, auth["allow"])
		require.Equal(t, false, auth["superuser"])
		acl, err := s.ACL(ctx, "source:42:credential:user", "node-red-shortcut", "$tier0/source/42/credential/data", "publish")
		require.NoError(t, err)
		require.Equal(t, allow, acl["allow"])
	}
	broken := errors.New("credential store unavailable")
	s.SetSourceAccess(sourceAccessFixture{err: broken})
	_, err := s.Auth(ctx, "source:42:credential:user", "secret", "device", "")
	require.ErrorIs(t, err, broken)
	_, err = s.ACL(ctx, "source:42:credential:user", "device", "topic", "subscribe")
	require.ErrorIs(t, err, broken)
	for _, topic := range []string{"$tier0", "$tier0/#", "$tier0/source/42/credential/command", "$share/group/$tier0/#", "$queue/$tier0/#"} {
		for _, username := range []string{"external", "open", "open:1"} {
			acl, err := s.ACL(ctx, username, "node-red-shortcut", topic, "subscribe")
			require.NoError(t, err)
			require.Equal(t, false, acl["allow"], "%s %s", username, topic)
		}
		acl, err := s.ACL(ctx, "backend", "platform-bridge", topic, "subscribe")
		require.NoError(t, err)
		require.Equal(t, true, acl["allow"])
	}
	for _, topic := range []string{"#", "+/data", "ordinary/uns/topic", "$tier00/topic"} {
		require.False(t, reservedSourceTopic(topic))
	}
}
