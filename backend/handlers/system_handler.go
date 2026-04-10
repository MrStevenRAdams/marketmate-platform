package handlers

// ============================================================================
// SYSTEM HANDLER
// ============================================================================
// Lightweight endpoints for operational visibility:
//
//   GET  /api/v1/system/memory  — current Go runtime memory stats
//
// Used by the SchemaCacheManager UI to display memory usage every 10 seconds
// so operators can distinguish between "job stuck" and "server OOM".
//
// No authentication bypass — all routes go through the standard tenant
// middleware stack, same as every other handler.
// ============================================================================

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

type SystemHandler struct{}

func NewSystemHandler() *SystemHandler {
	return &SystemHandler{}
}

// MemoryStats is the JSON shape returned by GET /api/v1/system/memory.
type MemoryStats struct {
	// Bytes allocated and still in use by the heap.
	HeapAllocBytes uint64 `json:"heapAllocBytes"`

	// Total bytes obtained from the OS for the heap (includes freed memory
	// not yet returned to the OS).
	HeapSysBytes uint64 `json:"heapSysBytes"`

	// Bytes of allocated heap objects that are currently in use (live objects).
	HeapInUseBytes uint64 `json:"heapInUseBytes"`

	// Bytes of heap memory currently sitting idle (returned to the OS pool
	// but not yet unmapped).
	HeapIdleBytes uint64 `json:"heapIdleBytes"`

	// Total bytes allocated over the lifetime of the process (monotonically
	// increasing; useful for leak detection).
	TotalAllocBytes uint64 `json:"totalAllocBytes"`

	// Number of live heap objects.
	HeapObjects uint64 `json:"heapObjects"`

	// Number of completed GC cycles.
	NumGC uint32 `json:"numGC"`

	// Fraction of CPU time used by the GC in the last GC cycle.
	GCCPUFraction float64 `json:"gcCpuFraction"`

	// Timestamp of this snapshot (RFC3339).
	SampledAt time.Time `json:"sampledAt"`

	// Convenience fields pre-converted to MiB for the frontend.
	HeapAllocMiB  float64 `json:"heapAllocMiB"`
	HeapSysMiB    float64 `json:"heapSysMiB"`
	HeapInUseMiB  float64 `json:"heapInUseMiB"`
}

// GetMemory returns a snapshot of Go runtime memory statistics.
//
// Route: GET /api/v1/system/memory
//
// Response: MemoryStats JSON (always 200 — errors are impossible here since
// runtime.ReadMemStats never fails).
func (h *SystemHandler) GetMemory(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	toMiB := func(b uint64) float64 {
		return float64(b) / (1024 * 1024)
	}

	stats := MemoryStats{
		HeapAllocBytes:  m.HeapAlloc,
		HeapSysBytes:    m.HeapSys,
		HeapInUseBytes:  m.HeapInuse,
		HeapIdleBytes:   m.HeapIdle,
		TotalAllocBytes: m.TotalAlloc,
		HeapObjects:     m.HeapObjects,
		NumGC:           m.NumGC,
		GCCPUFraction:   m.GCCPUFraction,
		SampledAt:       time.Now(),
		HeapAllocMiB:    toMiB(m.HeapAlloc),
		HeapSysMiB:      toMiB(m.HeapSys),
		HeapInUseMiB:    toMiB(m.HeapInuse),
	}

	c.JSON(http.StatusOK, stats)
}
