package buffer

import (
	"encoding/json"
	"sync"
	"time"

	"minIODB/internal/wal"
	"minIODB/pkg/logger"

	"go.uber.org/zap"
)

type walRecord struct {
	ID        string    `json:"id"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Table     string    `json:"table,omitempty"`
}

type DurableManager struct {
	buffer         []DataPoint
	mutex          sync.RWMutex
	maxSize        int
	flushCh        chan struct{}
	wal            *wal.WAL
	lastFlushedSeq uint64
	table          string
}

type DurableConfig struct {
	MaxSize     int
	WALDir      string
	Table       string
	SyncOnWrite bool
}

func NewDurableManager(cfg *DurableConfig) (*DurableManager, error) {
	walCfg := wal.DefaultConfig(cfg.WALDir)
	walCfg.SyncOnWrite = cfg.SyncOnWrite

	w, err := wal.New(walCfg)
	if err != nil {
		return nil, err
	}

	dm := &DurableManager{
		buffer:  make([]DataPoint, 0, cfg.MaxSize),
		maxSize: cfg.MaxSize,
		flushCh: make(chan struct{}, 1),
		wal:     w,
		table:   cfg.Table,
	}

	if err := dm.recover(); err != nil {
		logger.GetLogger().Warn("Failed to recover from WAL", zap.Error(err))
	}

	return dm, nil
}

func (dm *DurableManager) recover() error {
	recoveredCount := 0

	err := dm.wal.Replay(func(record *wal.Record) error {
		if record.Type != wal.RecordTypeData {
			return nil
		}

		var wr walRecord
		if err := json.Unmarshal(record.Data, &wr); err != nil {
			logger.GetLogger().Warn("Failed to unmarshal WAL record", zap.Error(err))
			return nil
		}

		if dm.table != "" && wr.Table != "" && wr.Table != dm.table {
			return nil
		}

		dm.buffer = append(dm.buffer, DataPoint{
			ID:        wr.ID,
			Data:      wr.Data,
			Timestamp: wr.Timestamp,
		})

		if record.SeqNum > dm.lastFlushedSeq {
			dm.lastFlushedSeq = record.SeqNum
		}

		recoveredCount++
		return nil
	})

	if recoveredCount > 0 {
		logger.GetLogger().Info("Recovered data from WAL",
			zap.Int("records", recoveredCount),
			zap.String("table", dm.table))
	}

	return err
}

func (dm *DurableManager) Add(id string, data []byte, timestamp time.Time) (bool, error) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if len(dm.buffer) >= dm.maxSize {
		return false, nil
	}

	wr := walRecord{
		ID:        id,
		Data:      data,
		Timestamp: timestamp,
		Table:     dm.table,
	}

	walData, err := json.Marshal(wr)
	if err != nil {
		return false, err
	}

	seqNum, err := dm.wal.Append(wal.RecordTypeData, walData)
	if err != nil {
		return false, err
	}

	dm.buffer = append(dm.buffer, DataPoint{
		ID:        id,
		Data:      data,
		Timestamp: timestamp,
	})

	dm.lastFlushedSeq = seqNum

	if len(dm.buffer) >= dm.maxSize {
		select {
		case dm.flushCh <- struct{}{}:
		default:
		}
	}

	return true, nil
}

func (dm *DurableManager) Flush(flushFunc FlushFunc) error {
	dm.mutex.Lock()
	if len(dm.buffer) == 0 {
		dm.mutex.Unlock()
		return nil
	}

	data := make([]DataPoint, len(dm.buffer))
	copy(data, dm.buffer)
	seqAtFlush := dm.lastFlushedSeq

	dm.buffer = dm.buffer[:0]
	dm.mutex.Unlock()

	if err := flushFunc(data); err != nil {
		dm.mutex.Lock()
		dm.buffer = append(data, dm.buffer...)
		dm.mutex.Unlock()
		return err
	}

	if err := dm.wal.Truncate(seqAtFlush); err != nil {
		logger.GetLogger().Warn("Failed to truncate WAL", zap.Error(err))
	}

	return nil
}

func (dm *DurableManager) Size() int {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	return len(dm.buffer)
}

func (dm *DurableManager) IsFull() bool {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	return len(dm.buffer) >= dm.maxSize
}

func (dm *DurableManager) Clear() {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	dm.buffer = dm.buffer[:0]
}

func (dm *DurableManager) FlushChannel() <-chan struct{} {
	return dm.flushCh
}

func (dm *DurableManager) Close() error {
	return dm.wal.Close()
}

func (dm *DurableManager) WALDir() string {
	return dm.wal.Dir()
}
