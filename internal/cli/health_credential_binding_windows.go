//go:build windows

package cli

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateHealthCredentialBindingFile(file *os.File) bool {
	if file == nil {
		return false
	}
	security, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || security == nil {
		return false
	}
	owner, _, err := security.Owner()
	if err != nil || owner == nil {
		return false
	}
	token := windows.GetCurrentProcessToken()
	defer token.Close()
	tokenUser, err := token.GetTokenUser()
	if err != nil || tokenUser == nil || tokenUser.User.Sid == nil || !windows.EqualSid(owner, tokenUser.User.Sid) {
		return false
	}
	dacl, _, err := security.DACL()
	if err != nil || dacl == nil || dacl.AceCount == 0 {
		return false
	}
	currentUser := tokenUser.User.Sid
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false
	}
	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	currentUserAllowed := false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, index, &ace) != nil || ace == nil {
			return false
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			return false
		}
		sid := (*windows.SID)(unsafe.Add(unsafe.Pointer(ace), unsafe.Offsetof(ace.SidStart)))
		if windows.EqualSid(sid, currentUser) {
			currentUserAllowed = true
			continue
		}
		if windows.EqualSid(sid, systemSID) || windows.EqualSid(sid, adminSID) {
			continue
		}
		return false
	}
	return currentUserAllowed
}
