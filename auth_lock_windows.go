//go:build windows

package republic

import (
	"context"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
var unlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")

func lockCredentials(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	overlapped := &syscall.Overlapped{}
	for {
		ok, _, callErr := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		if ok != 0 {
			return func() { unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(overlapped))); _ = f.Close() }, nil
		}
		if callErr != syscall.Errno(33) {
			_ = f.Close()
			return nil, callErr
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
