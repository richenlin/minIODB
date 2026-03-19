package backup

import (
	"context"
	"fmt"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/internal/dashboard/sse"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type Scheduler struct {
	plans    map[string]*config.BackupSchedule
	executor *Executor
	cron     *cron.Cron
	store    PlanStore
	hub      *sse.Hub
	mu       sync.RWMutex
	logger   *zap.Logger

	entryIDs map[string]cron.EntryID
	running  bool
	stopCh   chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewScheduler(
	executor *Executor,
	store PlanStore,
	hub *sse.Hub,
	logger *zap.Logger,
) *Scheduler {
	return &Scheduler{
		plans:    make(map[string]*config.BackupSchedule),
		executor: executor,
		cron:     cron.New(cron.WithSeconds()),
		store:    store,
		hub:      hub,
		logger:   logger,
		entryIDs: make(map[string]cron.EntryID),
		stopCh:   make(chan struct{}),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler already running")
	}

	s.stopCh = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(ctx)

	if s.store != nil {
		plans, err := s.store.ListPlans(ctx)
		if err != nil {
			s.logger.Warn("failed to load plans from store, starting with empty plans", zap.Error(err))
		} else {
			for _, plan := range plans {
				if plan.Enabled {
					s.plans[plan.ID] = plan
				}
			}
		}
	}

	for id, plan := range s.plans {
		if err := s.schedulePlan(plan); err != nil {
			s.logger.Error("failed to schedule plan",
				zap.String("plan_id", id),
				zap.Error(err))
		}
	}

	s.cron.Start()
	s.running = true

	s.logger.Info("backup scheduler started",
		zap.Int("plans_count", len(s.plans)))

	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	if s.cancel != nil {
		s.cancel()
	}
	close(s.stopCh)
	s.cron.Stop()
	s.entryIDs = make(map[string]cron.EntryID)
	s.running = false

	s.logger.Info("backup scheduler stopped")
}

func (s *Scheduler) AddPlan(plan *config.BackupSchedule) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if plan.ID == "" {
		return fmt.Errorf("plan ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.plans[plan.ID]; ok {
		if s.running && existing.Enabled {
			if entryID, exists := s.entryIDs[plan.ID]; exists {
				s.cron.Remove(entryID)
				delete(s.entryIDs, plan.ID)
			}
		}
	}

	if plan.Enabled && s.running {
		if err := s.schedulePlan(plan); err != nil {
			return fmt.Errorf("schedule plan: %w", err)
		}
	}

	planCopy := clonePlan(plan)
	s.plans[plan.ID] = planCopy

	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.SavePlan(ctx, plan); err != nil {
			s.logger.Warn("failed to persist plan", zap.Error(err))
		}
	}

	s.publishEvent("plan_added", map[string]interface{}{
		"plan_id": plan.ID,
		"name":    plan.Name,
	})

	s.logger.Info("backup plan added",
		zap.String("plan_id", plan.ID),
		zap.String("name", plan.Name))

	return nil
}

var ErrPlanAlreadyExists = fmt.Errorf("plan already exists")

func (s *Scheduler) AddPlanIfAbsent(plan *config.BackupSchedule) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if plan.ID == "" {
		return fmt.Errorf("plan ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.plans[plan.ID]; exists {
		return ErrPlanAlreadyExists
	}

	if plan.Enabled && s.running {
		if err := s.schedulePlan(plan); err != nil {
			return fmt.Errorf("schedule plan: %w", err)
		}
	}

	planCopyIfAbsent := clonePlan(plan)
	s.plans[plan.ID] = planCopyIfAbsent

	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.SavePlan(ctx, plan); err != nil {
			s.logger.Warn("failed to persist plan", zap.Error(err))
		}
	}

	s.publishEvent("plan_added", map[string]interface{}{
		"plan_id": plan.ID,
		"name":    plan.Name,
	})

	s.logger.Info("backup plan added",
		zap.String("plan_id", plan.ID),
		zap.String("name", plan.Name))

	return nil
}

func (s *Scheduler) RemovePlan(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, ok := s.plans[id]
	if !ok {
		return fmt.Errorf("plan not found: %s", id)
	}

	if s.running && plan.Enabled {
		if entryID, exists := s.entryIDs[id]; exists {
			s.cron.Remove(entryID)
			delete(s.entryIDs, id)
		}
	}

	delete(s.plans, id)

	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.DeletePlan(ctx, id); err != nil {
			s.logger.Warn("failed to delete plan from store", zap.Error(err))
		}
	}

	s.publishEvent("plan_removed", map[string]interface{}{
		"plan_id": id,
	})

	s.logger.Info("backup plan removed", zap.String("plan_id", id))

	return nil
}

func (s *Scheduler) TriggerExecution(id string) error {
	s.mu.RLock()
	plan, ok := s.plans[id]
	if !ok {
		s.mu.RUnlock()
		return fmt.Errorf("plan not found: %s", id)
	}
	planCopy := clonePlan(plan)
	s.mu.RUnlock()

	s.logger.Info("manual trigger for backup plan", zap.String("plan_id", id))

	go s.executePlan(planCopy)

	return nil
}

func (s *Scheduler) ListPlans() []*config.BackupSchedule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plans := make([]*config.BackupSchedule, 0, len(s.plans))
	for _, plan := range s.plans {
		planCopy := *plan
		plans = append(plans, &planCopy)
	}
	return plans
}

func (s *Scheduler) schedulePlan(plan *config.BackupSchedule) error {
	if !plan.Enabled {
		return nil
	}

	var schedule cron.Schedule
	var err error

	if plan.CronExpr != "" {
		parser := cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		schedule, err = parser.Parse(plan.CronExpr)
		if err != nil {
			schedule, err = cron.ParseStandard(plan.CronExpr)
			if err != nil {
				return fmt.Errorf("invalid cron expression '%s': %w", plan.CronExpr, err)
			}
		}
	} else if plan.Interval > 0 {
		schedule = &intervalSchedule{interval: plan.Interval}
	} else {
		return fmt.Errorf("plan must have either cron_expr or interval")
	}

	planCopy := clonePlan(plan)

	entryID := s.cron.Schedule(schedule, cron.FuncJob(func() {
		s.executePlan(planCopy)
	}))

	s.entryIDs[plan.ID] = entryID

	s.logger.Info("scheduled backup plan",
		zap.String("plan_id", plan.ID),
		zap.String("name", plan.Name),
		zap.Duration("interval", plan.Interval),
		zap.String("cron_expr", plan.CronExpr))

	return nil
}

func (s *Scheduler) executePlan(plan *config.BackupSchedule) {
	execID := fmt.Sprintf("exec-%s-%d", plan.ID, time.Now().Unix())

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	execution := &BackupExecution{
		ID:        execID,
		PlanID:    plan.ID,
		Status:    ExecutionStatusRunning,
		StartTime: time.Now(),
	}

	if s.executor == nil {
		execution.Status = ExecutionStatusFailed
		execution.Error = "executor not configured"
		now := time.Now()
		execution.EndTime = &now

		if s.store != nil {
			storeCtx, storeCancel := context.WithTimeout(ctx, 5*time.Second)
			s.store.SaveExecution(storeCtx, execution)
			storeCancel()
		}

		s.logger.Error("executor is nil, cannot execute backup plan",
			zap.String("plan_id", plan.ID))

		s.publishEvent("execution_failed", map[string]interface{}{
			"execution_id": execID,
			"plan_id":      plan.ID,
			"error":        "executor not configured",
		})
		return
	}

	if s.store != nil {
		storeCtx, storeCancel := context.WithTimeout(ctx, 5*time.Second)
		s.store.SaveExecution(storeCtx, execution)
		storeCancel()
	}

	s.publishEvent("execution_started", map[string]interface{}{
		"execution_id": execID,
		"plan_id":      plan.ID,
		"plan_name":    plan.Name,
	})

	var result *BackupResult
	var execErr error

	switch plan.BackupType {
	case BackupTypeFull:
		result, execErr = s.executor.FullBackup(ctx, "", plan.ID)
	case BackupTypeTable:
		if len(plan.Tables) == 0 {
			execErr = fmt.Errorf("no tables specified for table backup")
		} else {
			var errs []error
			for _, tableName := range plan.Tables {
				var tableResult *BackupResult
				var tableErr error
				tableResult, tableErr = s.executor.TableBackup(ctx, tableName, plan.ID)
				if tableErr != nil {
					s.logger.Warn("table backup failed",
						zap.String("plan_id", plan.ID),
						zap.String("table", tableName),
						zap.Error(tableErr))
					errs = append(errs, fmt.Errorf("table %s: %w", tableName, tableErr))
					continue
				}
				if result == nil {
					result = tableResult
				}
			}
			if len(errs) > 0 {
				execErr = fmt.Errorf("table backup errors: %v", errs)
			}
		}
	case BackupTypeMetadata:
		result, execErr = s.executor.MetadataBackup(ctx, plan.ID)
	default:
		execErr = fmt.Errorf("unknown backup type: %s", plan.BackupType)
	}

	if execErr != nil {
		execution.Status = ExecutionStatusFailed
		execution.Error = execErr.Error()

		s.logger.Error("backup execution failed",
			zap.String("execution_id", execID),
			zap.String("plan_id", plan.ID),
			zap.Error(execErr))
	} else {
		execution.Status = ExecutionStatusCompleted
		if result != nil {
			execution.Manifest = result.MetadataFile
			s.logger.Info("backup execution completed",
				zap.String("execution_id", execID),
				zap.String("plan_id", plan.ID),
				zap.String("backup_id", result.BackupID))
		} else {
			s.logger.Info("backup execution completed",
				zap.String("execution_id", execID),
				zap.String("plan_id", plan.ID))
		}
	}

	now := time.Now()
	execution.EndTime = &now

	if s.store != nil {
		storeCtx, storeCancel := context.WithTimeout(ctx, 5*time.Second)
		s.store.SaveExecution(storeCtx, execution)
		storeCancel()
	}

	s.publishEvent("execution_completed", map[string]interface{}{
		"execution_id": execID,
		"plan_id":      plan.ID,
		"status":       execution.Status,
		"error":        execution.Error,
	})
}

func (s *Scheduler) publishEvent(eventType string, data interface{}) {
	if s.hub == nil {
		return
	}

	s.hub.Publish("backup_scheduler", map[string]interface{}{
		"event": eventType,
		"data":  data,
		"time":  time.Now().Unix(),
	})
}

func clonePlan(plan *config.BackupSchedule) *config.BackupSchedule {
	if plan == nil {
		return nil
	}
	return &config.BackupSchedule{
		ID:            plan.ID,
		Name:          plan.Name,
		Enabled:       plan.Enabled,
		Interval:      plan.Interval,
		CronExpr:      plan.CronExpr,
		BackupType:    plan.BackupType,
		Tables:        append([]string{}, plan.Tables...),
		RetentionDays: plan.RetentionDays,
		CreatedAt:     plan.CreatedAt,
		UpdatedAt:     plan.UpdatedAt,
	}
}

type intervalSchedule struct {
	interval time.Duration
}

func (s *intervalSchedule) Next(t time.Time) time.Time {
	return t.Add(s.interval)
}
