package throttle

type LoadLevel int

const (
	LoadIdle LoadLevel = iota
	LoadNormal
	LoadBusy
	LoadOverloaded
)

func (l LoadLevel) String() string {
	switch l {
	case LoadIdle:
		return "idle"
	case LoadNormal:
		return "normal"
	case LoadBusy:
		return "busy"
	case LoadOverloaded:
		return "overloaded"
	default:
		return "unknown"
	}
}

type LoadLevelProvider interface {
	GetLoadLevel() LoadLevel
	GetThrottleRatio() float64
}

type LoadThresholds struct {
	GoroutineIdle       int
	GoroutineNormal     int
	GoroutineBusy       int
	MemoryRatioIdle     float64
	MemoryRatioNormal   float64
	MemoryRatioBusy     float64
	PoolUsageIdle       float64
	PoolUsageNormal     float64
	PoolUsageBusy       float64
	GCPauseMsIdle       float64
	GCPauseMsNormal     float64
	GCPauseMsBusy       float64
	PendingWritesIdle   int64
	PendingWritesNormal int64
	PendingWritesBusy   int64
}

func DefaultLoadThresholds() LoadThresholds {
	return LoadThresholds{
		GoroutineIdle:       500,
		GoroutineNormal:     2000,
		GoroutineBusy:       5000,
		MemoryRatioIdle:     0.3,
		MemoryRatioNormal:   0.6,
		MemoryRatioBusy:     0.8,
		PoolUsageIdle:       0.3,
		PoolUsageNormal:     0.6,
		PoolUsageBusy:       0.8,
		GCPauseMsIdle:       5,
		GCPauseMsNormal:     20,
		GCPauseMsBusy:       50,
		PendingWritesIdle:   100,
		PendingWritesNormal: 1000,
		PendingWritesBusy:   5000,
	}
}
