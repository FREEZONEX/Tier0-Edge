package privatefs

import (
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

// Windows mode bits are synthetic. Require an ACL restricted to the current
// process user, LocalSystem and administrators; never accept a null DACL.
func IsPrivate(path string, info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (sid.Equals(user.User.Sid) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return false
		}
	}
	return true
}

// File contents are flushed before rename. Windows does not support os.File.Sync
// on directory handles; POSIX directory fsync remains enabled on other platforms.
func SyncDir(_ string) error { return nil }
