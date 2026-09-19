//go:build windows

package supervisor

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

const (
	createNewProcessGroup             = 0x00000200
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createJobObject          = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	generateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
	closeHandle              = kernel32.NewProc("CloseHandle")
	jobMu                    sync.Mutex
	jobs                     = map[int]syscall.Handle{}
)

type ioCounters struct{ ReadOperationCount, WriteOperationCount, OtherOperationCount, ReadTransferCount, WriteTransferCount, OtherTransferCount uint64 }
type basicLimitInformation struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}
type extendedLimitInformation struct {
	BasicLimitInformation                                                        basicLimitInformation
	IoInfo                                                                       ioCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

func prepareProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	return nil
}
func attachProcess(cmd *exec.Cmd) error {
	h, _, e := createJobObject.Call(0, 0)
	if h == 0 {
		return e
	}
	info := extendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, e := setInformationJobObject.Call(h, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		closeHandle.Call(h)
		return e
	}
	process, err := syscall.OpenProcess(0x0001|0x0100|0x0400, false, uint32(cmd.Process.Pid))
	if err != nil {
		closeHandle.Call(h)
		return err
	}
	defer syscall.CloseHandle(process)
	r, _, e = assignProcessToJobObject.Call(h, uintptr(process))
	if r == 0 {
		closeHandle.Call(h)
		return e
	}
	jobMu.Lock()
	jobs[cmd.Process.Pid] = syscall.Handle(h)
	jobMu.Unlock()
	return nil
}
func releaseProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	jobMu.Lock()
	h := jobs[cmd.Process.Pid]
	delete(jobs, cmd.Process.Pid)
	jobMu.Unlock()
	if h != 0 {
		closeHandle.Call(uintptr(h))
	}
}
func gracefulProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	r, _, e := generateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(cmd.Process.Pid))
	if r == 0 {
		return e
	}
	return nil
}
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	jobMu.Lock()
	h := jobs[cmd.Process.Pid]
	delete(jobs, cmd.Process.Pid)
	jobMu.Unlock()
	if h != 0 {
		r, _, e := closeHandle.Call(uintptr(h))
		if r == 0 {
			return e
		}
		return nil
	}
	return cmd.Process.Kill()
}
