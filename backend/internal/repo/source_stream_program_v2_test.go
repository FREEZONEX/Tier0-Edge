package repo_test

// 验证入口身份及同 Topic 多次输出不会被去重身份折叠。
import (
	"testing"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestProgramV2Identities(t *testing.T) {
	a := repo.ProgramRunIdentity("message", 11, 12, "entry-a")
	require.Len(t, a, 64)
	require.Equal(t, a, repo.ProgramTaskIdentity("message", 11, 12, "entry-a"))
	require.NotEqual(t, a, repo.ProgramRunIdentity("message", 11, 12, "entry-b"))
	require.NotEqual(t, a, repo.ProgramRunIdentity("message", 11, 13, "entry-a"))
	require.NotEqual(t, repo.ProgramRunIdentity("a:b", 1, 2, "c"), repo.ProgramRunIdentity("a", 1, 2, "b:c"))
	require.NotEqual(t, repo.ProgramOutputIdentity(a, 0, 51), repo.ProgramOutputIdentity(a, 1, 51))
}
