package service

import (
	"litterbox-agent/internal/model"
	"litterbox-agent/internal/utils"
	"runtime"
	"sync/atomic"
	"time"
)

type MetricsService struct {
	requestCount  *uint64
	commandCount  *uint64
	uploadCount   *uint64
	downloadCount *uint64
	startTime     time.Time
}

func NewMetricsService() *MetricsService {
	var rc, cc, uc, dc uint64
	return &MetricsService{
		requestCount:  &rc,
		commandCount:  &cc,
		uploadCount:   &uc,
		downloadCount: &dc,
		startTime:     time.Now(),
	}
}

func (s *MetricsService) IncrementRequest() {
	atomic.AddUint64(s.requestCount, 1)
}

func (s *MetricsService) IncrementCommand() {
	atomic.AddUint64(s.commandCount, 1)
}

func (s *MetricsService) IncrementUpload() {
	atomic.AddUint64(s.uploadCount, 1)
}

func (s *MetricsService) IncrementDownload() {
	atomic.AddUint64(s.downloadCount, 1)
}

func (s *MetricsService) GetMetrics() *model.Metrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取CPU使用率
	cpuPercent := utils.GetCPUPercent()

	// 获取系统内存信息
	systemUsedMem, systemTotalMem := utils.GetSystemMemory()

	return &model.Metrics{
		Uptime:           time.Since(s.startTime).String(),
		RequestCount:     atomic.LoadUint64(s.requestCount),
		CommandCount:     atomic.LoadUint64(s.commandCount),
		UploadCount:      atomic.LoadUint64(s.uploadCount),
		DownloadCount:    atomic.LoadUint64(s.downloadCount),
		Goroutines:       runtime.NumGoroutine(),
		MemoryMB:         m.Alloc / 1024 / 1024,
		CPUPercent:       cpuPercent,
		SystemMemoryMB:   systemUsedMem,
		SystemTotalMemMB: systemTotalMem,
	}
}
