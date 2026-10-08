package main

import (
	"bytes"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ringBuffer is a concurrency-safe capped byte buffer that drops the oldest
// data once it exceeds its maximum size.
type ringBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newRingBuffer(max int) *ringBuffer {
	if max <= 0 {
		max = 256 * 1024
	}
	return &ringBuffer{max: max}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, p...)
	if len(r.data) > r.max {
		r.data = append([]byte(nil), r.data[len(r.data)-r.max:]...)
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.data)
}

// Process manages a single frp child process.
type Process struct {
	name       string
	binPath    string
	configPath string
	workDir    string

	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	log       *ringBuffer
	startTime time.Time
	lastExit  string
}

func newProcess(name, binPath, configPath, workDir string, logMax int) *Process {
	return &Process{
		name:       name,
		binPath:    binPath,
		configPath: configPath,
		workDir:    workDir,
		log:        newRingBuffer(logMax),
	}
}

// Start launches the process if it is not already running.
func (p *Process) Start() error {
	p.mu.Lock()
	if p.cmd != nil {
		p.mu.Unlock()
		return nil
	}
	cmd := exec.Command(p.binPath, "-c", p.configPath)
	if p.workDir != "" {
		cmd.Dir = p.workDir
	}
	cmd.Stdout = p.log
	cmd.Stderr = p.log
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.cmd = cmd
	p.startTime = time.Now()
	p.lastExit = ""
	done := make(chan struct{})
	p.done = done
	p.mu.Unlock()

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
			p.startTime = time.Time{}
			if err != nil {
				p.lastExit = err.Error()
			} else {
				p.lastExit = "exited normally"
			}
		}
		p.mu.Unlock()
		close(done)
	}()
	return nil
}

// Stop terminates the process gracefully, then forcefully if needed. It blocks
// until the process has actually exited.
func (p *Process) Stop() {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

// Restart stops (if running) and starts the process again.
func (p *Process) Restart() error {
	p.Stop()
	return p.Start()
}

// Status returns the current state of the process.
func (p *Process) Status() ProcessStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := ProcessStatus{
		Name:    p.name,
		Running: p.cmd != nil,
	}
	if p.cmd != nil && p.cmd.Process != nil {
		st.PID = p.cmd.Process.Pid
		st.StartTime = p.startTime.Format(time.RFC3339)
	}
	if !st.Running && p.lastExit != "" {
		st.LastExit = p.lastExit
	}
	return st
}

func (p *Process) Log() string {
	return p.log.String()
}

// ProcessStatus is the JSON representation returned to the frontend.
type ProcessStatus struct {
	Name      string `json:"name"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	StartTime string `json:"startTime,omitempty"`
	LastExit  string `json:"lastExit,omitempty"`
}

// readOutput is a helper used by the update flow to run a binary and capture
// its combined output (e.g. `frps --version`).
func readOutput(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
