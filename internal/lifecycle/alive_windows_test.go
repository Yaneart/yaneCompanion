package lifecycle

import "syscall"

func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if syscall.GetExitCodeProcess(handle, &code) != nil {
		return false
	}
	return code == 259
}
