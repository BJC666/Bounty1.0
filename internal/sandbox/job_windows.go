//go:build windows

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// JobOptions configures process containment for one bash execution.
type JobOptions struct {
	WorkspaceRoot string
	AllowWrite    []string
	ForbidRead    []string
	ForbidWrite   []string
	Network       bool
}

// Container keeps a Job Object handle alive for the lifetime of a contained
// process. Closing it (KILL_ON_JOB_CLOSE) terminates every process in the
// job — children spawned by the command cannot escape.
type Container struct {
	job windows.Handle
}

// StartContained starts cmd suspended, attaches it to a fresh Job Object
// with KILL_ON_JOB_CLOSE and no breakaway, then resumes its threads. The
// returned Container must be kept until the command finishes and closed
// afterwards; closing it reaps any escaped children.
func StartContained(cmd *exec.Cmd, opts JobOptions) (*Container, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	applySandboxEnv(cmd, opts)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox start: %w", err)
	}

	proc, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("sandbox open process: %w", err)
	}
	defer windows.CloseHandle(proc)

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("sandbox create job: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return nil, fmt.Errorf("sandbox configure job: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		cmd.Process.Kill()
		return nil, fmt.Errorf("sandbox assign job: %w", err)
	}

	resumeAllThreads(uint32(cmd.Process.Pid))
	return &Container{job: job}, nil
}

// Close closes the job handle, which terminates every process still in the
// job (KILL_ON_JOB_CLOSE).
func (c *Container) Close() error {
	if c == nil {
		return nil
	}
	return windows.CloseHandle(c.job)
}

// Kill terminates every process in the job immediately.
func (c *Container) Kill() error {
	if c == nil {
		return nil
	}
	return windows.TerminateJobObject(c.job, 1)
}

// resumeAllThreads resumes every thread belonging to pid (the process was
// created suspended so the Job Object could be attached first).
func resumeAllThreads(pid uint32) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err = windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		h, herr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if herr != nil {
			continue
		}
		windows.ResumeThread(h)
		windows.CloseHandle(h)
	}
}

// applySandboxEnv strips API keys and, when Network is off, points all proxy
// env vars at a dead local address so proxy-honoring tools (curl/pip/npm/
// python requests/...) fail fast instead of reaching the internet. This is
// env-level egress control; kernel-level (WFP) blocking is future work.
func applySandboxEnv(cmd *exec.Cmd, opts JobOptions) {
	cmd.Env = stripSecrets(cmd.Env)
	if opts.Network {
		return
	}
	dead := "http://127.0.0.1:9"
	var out []string
	for _, e := range cmd.Env {
		name := e
		if i := indexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		switch lowerASCII(name) {
		case "http_proxy", "https_proxy", "all_proxy", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
			continue
		case "no_proxy", "NO_PROXY":
			continue
		}
		out = append(out, e)
	}
	out = append(out,
		"HTTP_PROXY="+dead, "HTTPS_PROXY="+dead, "ALL_PROXY="+dead,
		"http_proxy="+dead, "https_proxy="+dead, "all_proxy="+dead,
		"NO_PROXY=", "no_proxy=",
	)
	cmd.Env = out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
