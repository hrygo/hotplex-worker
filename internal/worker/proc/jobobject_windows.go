//go:build windows

package proc

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CreateJobObject creates a Windows Job Object configured to kill all child
// processes when the handle is closed (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE).
func CreateJobObject() (uintptr, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("set job object info: %w", err)
	}

	return uintptr(job), nil
}

// AssignProcessToJob assigns a process to a Job Object.
func AssignProcessToJob(job uintptr, pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("open process %d for job assignment: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.AssignProcessToJobObject(windows.Handle(job), handle); err != nil {
		return fmt.Errorf("assign process %d to job: %w", pid, err)
	}
	return nil
}
