package center

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// 在创建时应用私有 DACL，不能先创建可继承 Users 权限的文件再改权限。
func createNetworkCredentialFile(dir string) (*os.File, error) {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "network-enrollment-"+uuid.NewString())
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

// Windows 的标准客户端是机器级单实例。只允许本安装管理的空客户端
// 首次入网，并持久绑定到一个网络身份域；不切换、复制或清空其他身份。
func prepareWindowsManagedNetwork(ctx context.Context, cfg config.Config, inspected fleetprotocol.NetworkBootstrapResponse, domain string, apply func() error) error {
	rejected := errors.New("FLEET_NETWORK_STATE_ISOLATION_REQUIRED")
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || !user.User.Sid.IsWellKnown(windows.WinLocalSystemSid) || cfg.InstallationID == "" || cfg.TailscaleSocket != "" {
		return rejected
	}
	expected := filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe")
	if !filepath.IsAbs(expected) || !strings.EqualFold(filepath.Clean(cfg.TailscaleBinary), expected) {
		return rejected
	}
	// 与原安装器/维护任务共用互斥锁。Win32 mutex 由线程拥有，不能跨 Go 线程释放。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, _ := windows.UTF16PtrFromString(`Global\Tier0FleetTailscaleInstall`)
	mutex, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return errors.New("FLEET_NETWORK_INITIALIZING")
	}
	defer windows.CloseHandle(mutex)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state, e := windows.WaitForSingleObject(mutex, 250)
		if e != nil {
			return rejected
		}
		if state == windows.WAIT_OBJECT_0 || state == windows.WAIT_ABANDONED {
			break
		}
		if state != uint32(windows.WAIT_TIMEOUT) {
			return rejected
		}
	}
	defer windows.ReleaseMutex(mutex)
	owner, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Tier0\FleetNetwork`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return rejected
	}
	defer owner.Close()
	installation, _, err := owner.GetStringValue("InstallationID")
	if err != nil || installation != cfg.InstallationID {
		return rejected
	}
	bound, _, err := owner.GetStringValue("StateDomain")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return rejected
	}
	if bound != "" && bound != domain {
		return rejected
	}
	current, err := readNetworkStatus(ctx, cfg)
	if err != nil {
		return err
	}
	server := strings.TrimRight(inspected.LoginServer, "/")
	if bound == "" {
		// 不根据 Self.ID 暂时为空就认定客户端全新；停止/未选中的 profile
		// 也必须被拒绝。已有云端绑定也不能在本机丢失状态后重建身份。
		profiles, e := exec.CommandContext(ctx, cfg.TailscaleBinary, "switch", "--list").Output()
		client, statusErr := readTailscaleClientStatus(ctx, cfg)
		if e != nil || statusErr != nil || client.BackendState != "NeedsLogin" || strings.Join(strings.Fields(string(profiles)), " ") != "ID Tailnet Account" ||
			current.State != "unjoined" || current.NodeID != "" || inspected.NetworkNodeID != "" {
			return rejected
		}
		// 在请求入网密钥之前写入持久栅栏；失败/断电后仍只允许同一目标重试。
		if err := owner.SetStringValue("StateDomain", domain); err != nil {
			return rejected
		}
	} else if current.NodeID != "" && (current.LoginServer != server || inspected.NetworkNodeID != "" && current.NodeID != inspected.NetworkNodeID) {
		return errors.New("FLEET_NETWORK_IDENTITY_MISMATCH")
	}
	status, _, _ := windows.NewLazySystemDLL("advapi32.dll").NewProc("RegFlushKey").Call(uintptr(owner))
	if status != 0 {
		return rejected
	}
	if err := ensureNetwork(ctx, cfg); err != nil {
		return err
	}
	joined, err := readNetworkStatus(ctx, cfg)
	if err != nil || joined.State != "online" || joined.NodeID == "" || joined.LoginServer != server ||
		(inspected.NetworkNodeID != "" && joined.NodeID != inspected.NetworkNodeID) {
		return errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
	}
	return apply()
}
