//go:build windows

package vault

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

type WindowsProtector struct{}

func (p WindowsProtector) Protect(data []byte) ([]byte, error)   { return dpapi(data, true) }
func (p WindowsProtector) Unprotect(data []byte) ([]byte, error) { return dpapi(data, false) }
func dpapi(data []byte, protect bool) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data))}
	if len(data) > 0 {
		in.Data = &data[0]
	}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}
func CurrentSID() (string, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
func applyACL(path, descriptor string) error {
	sd, err := windows.SecurityDescriptorFromString(descriptor)
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func secureFile(path string) error {
	sid, err := CurrentSID()
	if err != nil {
		return err
	}
	return applyACL(path, "D:P(A;;FA;;;SY)(A;;FA;;;"+sid+")")
}
func secureDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	sid, err := CurrentSID()
	if err != nil {
		return err
	}
	return applyACL(path, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;"+sid+")")
}
func replaceFile(src, dst string) error {
	a, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	b, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
func DefaultDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "AgentVault") }
