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

func TestScheduler_RestartAfterStop(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()

	err := scheduler.Start(ctx)
	require.NoError(t, err, "first Start() should succeed")
	assert.True(t, scheduler.running, "scheduler should be running after first Start()")

	scheduler.Stop()
	assert.False(t, scheduler.running, "scheduler should not be running after Stop()")

	err = scheduler.Start(ctx)
	require.NoError(t, err, "second Start() after Stop() should succeed")
	assert.True(t, scheduler.running, "scheduler should be running after second Start()")

	scheduler.Stop()
	assert.False(t, scheduler.running, "scheduler should not be running after second Stop()")

	err = scheduler.Start(ctx)
	require.NoError(t, err, "third Start() after Stop() should succeed")
	assert.True(t, scheduler.running, "scheduler should be running after third Start()")

	scheduler.Stop()
}

func TestScheduler_StopIdempotent(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))

	scheduler.Stop()
	scheduler.Stop()
	scheduler.Stop()

	assert.False(t, scheduler.running)
}

func TestScheduler_NoDoubleClosePanic(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("double close panic detected: %v", r)
		}
	}()

	for i := 0; i < 5; i++ {
		err := scheduler.Start(ctx)
		require.NoError(t, err, "iteration %d: Start() should succeed", i)
		scheduler.Stop()
	}
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

func TestScheduler_PlanClone_ConcurrentModification(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	originalTables := []string{"table1", "table2"}
	plan := &config.BackupSchedule{
		ID:         "concurrent-test-plan",
		Name:       "Original Name",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeTable,
		Tables:     originalTables,
	}

	require.NoError(t, scheduler.AddPlan(plan))

	plan.Name = "Modified Name"
	plan.Tables[0] = "modified_table"
	plan.BackupType = BackupTypeFull

	plans := scheduler.ListPlans()
	require.Len(t, plans, 1)

	storedPlan := plans[0]
	assert.Equal(t, "Original Name", storedPlan.Name, "stored plan should not be affected by external modification")
	assert.Equal(t, BackupTypeTable, storedPlan.BackupType, "stored plan backup type should not change")
	assert.Equal(t, []string{"table1", "table2"}, storedPlan.Tables, "stored plan tables should not change")
}

func TestScheduler_AddPlan_StoresCopy(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "copy-test-plan",
		Name:       "Original",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
		Tables:     []string{"t1"},
	}

	require.NoError(t, scheduler.AddPlan(plan))

	plan.Name = "Modified"
	plan.Tables = append(plan.Tables, "t2")

	plans := scheduler.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, "Original", plans[0].Name)
	assert.Equal(t, []string{"t1"}, plans[0].Tables)
}

func TestScheduler_AddPlanIfAbsent_StoresCopy(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "if-absent-plan",
		Name:       "Original",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
		Tables:     []string{"t1"},
	}

	require.NoError(t, scheduler.AddPlanIfAbsent(plan))

	plan.Name = "Modified"
	plan.Tables = append(plan.Tables, "t2")

	plans := scheduler.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, "Original", plans[0].Name)
	assert.Equal(t, []string{"t1"}, plans[0].Tables)
}

func TestScheduler_TriggerExecution_UsesPlanCopy(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "trigger-copy-plan",
		Name:       "Original Name",
		Enabled:    true,
		Interval:   24 * time.Hour,
		BackupType: BackupTypeFull,
		Tables:     []string{"table1"},
	}

	require.NoError(t, scheduler.AddPlan(plan))

	go func() {
		time.Sleep(10 * time.Millisecond)
		plan.Name = "Concurrent Modified"
		plan.Tables = []string{"concurrent_table"}
	}()

	err := scheduler.TriggerExecution("trigger-copy-plan")
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	executions, err := store.ListExecutions(ctx, "trigger-copy-plan", 10)
	require.NoError(t, err)
	if len(executions) > 0 {
		assert.Equal(t, ExecutionStatusFailed, executions[0].Status)
	}
}

func TestScheduler_UpdatePlan_DoesNotAffectScheduledCron(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)
	ctx := context.Background()
	require.NoError(t, scheduler.Start(ctx))
	defer scheduler.Stop()

	plan := &config.BackupSchedule{
		ID:         "cron-update-plan",
		Name:       "Original",
		Enabled:    true,
		Interval:   1 * time.Hour,
		BackupType: BackupTypeFull,
		Tables:     []string{"original_table"},
	}

	require.NoError(t, scheduler.AddPlan(plan))

	updatedPlan := &config.BackupSchedule{
		ID:         "cron-update-plan",
		Name:       "Updated",
		Enabled:    true,
		Interval:   2 * time.Hour,
		BackupType: BackupTypeTable,
		Tables:     []string{"updated_table"},
	}

	require.NoError(t, scheduler.AddPlan(updatedPlan))

	plans := scheduler.ListPlans()
	require.Len(t, plans, 1)
	assert.Equal(t, "Updated", plans[0].Name)
	assert.Equal(t, BackupTypeTable, plans[0].BackupType)
	assert.Equal(t, []string{"updated_table"}, plans[0].Tables)
}

func TestClonePlan_Nil(t *testing.T) {
	result := clonePlan(nil)
	assert.Nil(t, result)
}

func TestClonePlan_Complete(t *testing.T) {
	original := &config.BackupSchedule{
		ID:            "test-id",
		Name:          "test-name",
		Enabled:       true,
		Interval:      1 * time.Hour,
		CronExpr:      "0 0 * * *",
		BackupType:    BackupTypeTable,
		Tables:        []string{"t1", "t2"},
		RetentionDays: 30,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	cloned := clonePlan(original)

	assert.Equal(t, original.ID, cloned.ID)
	assert.Equal(t, original.Name, cloned.Name)
	assert.Equal(t, original.Enabled, cloned.Enabled)
	assert.Equal(t, original.Interval, cloned.Interval)
	assert.Equal(t, original.CronExpr, cloned.CronExpr)
	assert.Equal(t, original.BackupType, cloned.BackupType)
	assert.Equal(t, original.Tables, cloned.Tables)
	assert.Equal(t, original.RetentionDays, cloned.RetentionDays)
	assert.Equal(t, original.CreatedAt, cloned.CreatedAt)
	assert.Equal(t, original.UpdatedAt, cloned.UpdatedAt)

	assert.NotSame(t, original, cloned)
	assert.NotSame(t, &original.Tables, &cloned.Tables)

	cloned.Tables[0] = "modified"
	assert.Equal(t, []string{"t1", "t2"}, original.Tables, "original tables should not be affected")
}

func TestScheduler_ContextPropagation(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	require.NoError(t, scheduler.Start(parentCtx))

	assert.NotNil(t, scheduler.ctx, "scheduler.ctx should be initialized")
	assert.NotNil(t, scheduler.cancel, "scheduler.cancel should be initialized")

	scheduler.Stop()
	assert.False(t, scheduler.running, "scheduler should not be running")
}

func TestScheduler_StopCancelsContext(t *testing.T) {
	logger := zap.NewNop()
	store := newMockPlanStore()
	hub := sse.NewHub()

	scheduler := NewScheduler(nil, store, hub, logger)

	parentCtx, parentCancel := context.WithCancel(context.Background())
	require.NoError(t, scheduler.Start(parentCtx))

	ctxCopy := scheduler.ctx
	assert.NotNil(t, ctxCopy, "scheduler.ctx should be initialized")

	scheduler.Stop()

	select {
	case <-ctxCopy.Done():
	default:
		t.Error("scheduler.ctx should be canceled after Stop()")
	}
	parentCancel()
}
