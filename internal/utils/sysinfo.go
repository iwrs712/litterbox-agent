package utils

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	lastCPUTime   int64
	lastSysTime   int64
	lastCheckTime time.Time
)

// GetCPUPercent returns the CPU usage percentage
func GetCPUPercent() float64 {
	now := time.Now()

	// 首次调用，初始化
	if lastCheckTime.IsZero() {
		lastCheckTime = now
		var rusage syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
		lastCPUTime = rusage.Utime.Nano() + rusage.Stime.Nano()
		lastSysTime = now.UnixNano()
		return 0.0
	}

	var rusage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)

	currentCPUTime := rusage.Utime.Nano() + rusage.Stime.Nano()
	currentSysTime := now.UnixNano()

	cpuDelta := float64(currentCPUTime - lastCPUTime)
	sysDelta := float64(currentSysTime - lastSysTime)

	percent := 0.0
	if sysDelta > 0 {
		percent = (cpuDelta / sysDelta) * 100.0 * float64(runtime.NumCPU())
	}

	lastCPUTime = currentCPUTime
	lastSysTime = currentSysTime
	lastCheckTime = now

	// 限制在合理范围内
	if percent > 100.0*float64(runtime.NumCPU()) {
		percent = 100.0 * float64(runtime.NumCPU())
	}
	if percent < 0 {
		percent = 0
	}

	return percent
}

// GetSystemMemory returns system memory information in MB
func GetSystemMemory() (used, total uint64) {
	if runtime.GOOS == "linux" {
		return getLinuxMemory()
	} else if runtime.GOOS == "darwin" {
		return getDarwinMemory()
	}
	return 0, 0
}

// getLinuxMemory reads memory info from /proc/meminfo on Linux
func getLinuxMemory() (used, total uint64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var memTotal, memFree, memAvailable uint64

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch fields[0] {
		case "MemTotal:":
			memTotal = value / 1024 // Convert KB to MB
		case "MemFree:":
			memFree = value / 1024
		case "MemAvailable:":
			memAvailable = value / 1024
		}
	}

	if memAvailable > 0 {
		used = memTotal - memAvailable
	} else {
		used = memTotal - memFree
	}

	return used, memTotal
}

// getDarwinMemory gets memory info on macOS using sysctl-like approach
func getDarwinMemory() (used, total uint64) {
	// 在 macOS 上，我们使用 syscall 获取页面大小和物理内存
	// 但由于跨平台限制，这里简化实现
	// 可以通过执行 sysctl 命令获取更精确的值

	// 简单实现：返回0表示不可用，或者使用估算值
	// 生产环境建议使用 github.com/shirou/gopsutil 等第三方库
	return 0, 0
}
