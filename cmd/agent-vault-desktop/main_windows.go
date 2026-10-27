//go:build windows

// The desktop entry point starts the CLI host without opening a console window.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func main() {
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(filepath.Join(filepath.Dir(exe), "agent-vault.exe"), os.Args[1:]...)
		cmd.Dir = filepath.Dir(exe)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		err = cmd.Start()
	}
	if err != nil {
		text, _ := syscall.UTF16PtrFromString("无法启动 Agent Vault。请检查安装是否完整，或是否被 Windows 安全策略拦截。")
		title, _ := syscall.UTF16PtrFromString("Agent Vault")
		messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
		_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
		os.Exit(1)
	}
}
