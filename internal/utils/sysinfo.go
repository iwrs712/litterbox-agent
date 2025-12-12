package utils

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"os"
	"time"
)

var (
	lastCPUTimes  []cpu.TimesStat
	lastCheckTime time.Time
	pid           int32
)

func init() {
	pid = int32(os.Getpid())
}

// GetCPUPercent returns the CPU usage percentage of current process
func GetCPUPercent() float64 {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return 0.0
	}

	percent, err := proc.CPUPercent()
	if err != nil {
		return 0.0
	}

	return percent
}

// GetSystemCPUPercent returns the system-wide CPU usage percentage
func GetSystemCPUPercent() float64 {
	now := time.Now()

	// First call, initialize
	if lastCheckTime.IsZero() {
		lastCPUTimes, _ = cpu.Times(false)
		lastCheckTime = now
		return 0.0
	}

	// Get current CPU times
	currentCPUTimes, err := cpu.Times(false)
	if err != nil || len(currentCPUTimes) == 0 || len(lastCPUTimes) == 0 {
		return 0.0
	}

	// Calculate CPU usage
	last := lastCPUTimes[0]
	current := currentCPUTimes[0]

	totalDelta := current.Total() - last.Total()
	idleDelta := current.Idle - last.Idle

	percent := 0.0
	if totalDelta > 0 {
		percent = (totalDelta - idleDelta) / totalDelta * 100.0
	}

	// Update last values
	lastCPUTimes = currentCPUTimes
	lastCheckTime = now

	return percent
}

// GetSystemMemory returns system memory information in MB
func GetSystemMemory() (used, total uint64) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0
	}

	// Convert bytes to MB
	return v.Used / 1024 / 1024, v.Total / 1024 / 1024
}

// GetProcessMemory returns the memory usage of current process in MB
func GetProcessMemory() uint64 {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return 0
	}

	memInfo, err := proc.MemoryInfo()
	if err != nil {
		return 0
	}

	// Convert bytes to MB (RSS - Resident Set Size)
	return memInfo.RSS / 1024 / 1024
}
