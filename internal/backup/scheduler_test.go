package backup

import (
	"context"
	"sync"
	"testing"
	"time"

	"minIODB/config"
	"minIODB/internal/dashboard/sse"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockPlanStore struct {
	plans      map[string]*config.BackupSchedule
	executions map[string][]*BackupExecution
	mu         sync.RWMutex
}

func newMockPlanStore() *mockPlanStore {
	return &mockPlanStore{
		plans:      make(map[string]*config.BackupSchedule),
		executions: make(map[string][]*BackupExecution),
	}
}

func (m *mockPlanStore) ListPlans(ctx context.Context) ([]*config.BackupSchedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	plans := make([]*config.BackupSchedule, 0, len(m.plans))
	for _, p := range m.plans {
		plans = append(plans, p)
	}
	return plans, nil
}

func (m *mockPlanStore) SavePlan(ctx context.Context, plan *config.BackupSchedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[plan.ID] = plan
	return nil
}

func (m *mockPlanStore) DeletePlan(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.plans, id)
	delete(m.executions, id)
	return nil
}

func (m *mockPlanStore) ListExecutions(ctx context.Context, planID string, limit int) ([]*BackupExecution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	executions, ok := m.executions[planID]
	if !ok {
		return []*BackupExecution{}, nil
	}
	if limit > len(executions) {
		limit = len(executions)
	}
	return executions[:limit], nil
}

func (m *mockPlanStore) SaveExecution(ctx context.Context, exec *BackupExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executions[exec.PlanID] = append([]*BackupExecution{exec}, m.executions[exec.PlanID]...)
	return nil
}

func TestNewScheduler(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	assert.NotNil(t, scheduler)
	assert.NotNil(t, scheduler.plans)
	assert.NotNil(t, scheduler.cron)
	assert.False(t, scheduler.running)
}

func TestScheduler_StartStop(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	ctx := context.Background()
	err := scheduler.Start(ctx)
	require.NoError(t, err)
	assert.True(t, scheduler.running)

	err = scheduler.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	scheduler.Stop()
	assert.False(t, scheduler.running)
}

func TestScheduler_AddPlan(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "test-plan-1",
		Name:       "Test Plan",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}

	err := scheduler.AddPlan(plan)
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
	assert.Equal(t, "test-plan-1", plans[0].ID)

	storedPlans, _ := store.ListPlans(ctx)
	assert.Len(t, storedPlans, 1)
}

func TestScheduler_AddPlan_Nil(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	err := scheduler.AddPlan(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan is nil")
}

func TestScheduler_AddPlan_NoID(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	plan := &config.BackupSchedule{
		Name:    "Test Plan",
		Enabled: true,
	}

	err := scheduler.AddPlan(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan ID is required")
}

func TestScheduler_AddPlan_CronExpr(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "cron-plan-1",
		Name:       "Cron Plan",
		Enabled:    true,
		CronExpr:   "0 0 * * *",
		BackupType: BackupTypeFull,
	}

	err := scheduler.AddPlan(plan)
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
}

func TestScheduler_AddPlan_CronExprWithSeconds(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "cron-plan-2",
		Name:       "Cron Plan with Seconds",
		Enabled:    true,
		CronExpr:   "0 0 0 * * *",
		BackupType: BackupTypeFull,
	}

	err := scheduler.AddPlan(plan)
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
}

func TestScheduler_AddPlan_InvalidCronExpr(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "invalid-cron",
		Name:       "Invalid Cron",
		Enabled:    true,
		CronExpr:   "invalid-cron-expr",
		BackupType: BackupTypeFull,
	}

	err := scheduler.AddPlan(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron expression")
}

func TestScheduler_RemovePlan(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "remove-plan-1",
		Name:       "Remove Plan",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}

	require.NoError(t, scheduler.AddPlan(plan))

	err := scheduler.RemovePlan("remove-plan-1")
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 0)

	storedPlans, _ := store.ListPlans(ctx)
	assert.Len(t, storedPlans, 0)
}

func TestScheduler_RemovePlan_NotFound(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	err := scheduler.RemovePlan("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan not found")
}

func TestScheduler_ListPlans(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan1 := &config.BackupSchedule{
		ID:         "plan-1",
		Name:       "Plan 1",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}
	plan2 := &config.BackupSchedule{
		ID:         "plan-2",
		Name:       "Plan 2",
		Enabled:    true,
		Interval:   2 * time.Hour,
		BackupType: BackupTypeTable,
		Tables:     []string{"table1"},
	}

	require.NoError(t, scheduler.AddPlan(plan1))
	require.NoError(t, scheduler.AddPlan(plan2))

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 2)

	planMap := make(map[string]*config.BackupSchedule)
	for _, p := range plans {
		planMap[p.ID] = p
	}
	assert.Contains(t, planMap, "plan-1")
	assert.Contains(t, planMap, "plan-2")
}

func TestScheduler_TriggerExecution_PlanNotFound(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	err := scheduler.TriggerExecution("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan not found")
}

func TestScheduler_TriggerExecution_NilExecutor(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "trigger-plan-1",
		Name:       "Trigger Plan",
		Enabled:    true,
		Interval:   24 * time.Hour,
		BackupType: BackupTypeFull,
	}
	require.NoError(t, scheduler.AddPlan(plan))

	err := scheduler.TriggerExecution("trigger-plan-1")
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	executions, err := store.ListExecutions(ctx, "trigger-plan-1", 10)
	require.NoError(t, err)
	if len(executions) > 0 {
		assert.Equal(t, ExecutionStatusFailed, executions[0].Status)
		assert.Contains(t, executions[0].Error, "executor not configured")
	}
}

func TestScheduler_UpdatePlan(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "update-plan-1",
		Name:       "Original Name",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}
	require.NoError(t, scheduler.AddPlan(plan))

	updatedPlan := &config.BackupSchedule{
		ID:         "update-plan-1",
		Name:       "Updated Name",
		Enabled:    true,
		Interval:   2 * time.Hour,
		BackupType: BackupTypeTable,
		Tables:     []string{"table1"},
	}

	err := scheduler.AddPlan(updatedPlan)
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
	assert.Equal(t, "Updated Name", plans[0].Name)
	assert.Equal(t, 2*time.Hour, plans[0].Interval)
}

func TestScheduler_DisabledPlan(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "disabled-plan-1",
		Name:       "Disabled Plan",
		Enabled:    false,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}

	err := scheduler.AddPlan(plan)
	require.NoError(t, err)

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
	assert.False(t, plans[0].Enabled)
}

func TestScheduler_StartWithExistingPlans(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	existingPlan := &config.BackupSchedule{
		ID:         "existing-plan-1",
		Name:       "Existing Plan",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
	}
	require.NoError(t, store.SavePlan(context.Background(), existingPlan))

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plans := scheduler.ListPlans()
	assert.Len(t, plans, 1)
	assert.Equal(t, "existing-plan-1", plans[0].ID)
}

func TestIntervalSchedule_Next(t *testing.T) {
	interval := 1 * time.Hour
	schedule := &intervalSchedule{interval: interval}

	now := time.Now()
	next := schedule.Next(now)

	assert.Equal(t, now.Add(interval), next)
}
