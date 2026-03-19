package backup

import (
	"context"
	"testing"
	"time"

	"minIODB/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupExecution_Status(t *testing.T) {
	tests := []struct {
		name   string
		status ExecutionStatus
	}{
		{"pending", ExecutionStatusPending},
		{"running", ExecutionStatusRunning},
		{"completed", ExecutionStatusCompleted},
		{"failed", ExecutionStatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &BackupExecution{
				ID:     "test-id",
				Status: tt.status,
			}
			assert.Equal(t, tt.status, exec.Status)
		})
	}
}

func TestRedisPlanStore_Plans_Fallback(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()

	t.Run("empty plans initially", func(t *testing.T) {
		plans, err := store.ListPlans(ctx)
		assert.NoError(t, err)
		assert.Empty(t, plans)
	})

	t.Run("save and list plans", func(t *testing.T) {
		plan1 := &config.BackupSchedule{
			ID:         "plan-1",
			Name:       "Test Plan 1",
			Enabled:    true,
			BackupType: "metadata",
		}

		plan2 := &config.BackupSchedule{
			ID:         "plan-2",
			Name:       "Test Plan 2",
			Enabled:    false,
			BackupType: "full",
		}

		err := store.SavePlan(ctx, plan1)
		require.NoError(t, err)

		err = store.SavePlan(ctx, plan2)
		require.NoError(t, err)

		plans, err := store.ListPlans(ctx)
		assert.NoError(t, err)
		assert.Len(t, plans, 2)
	})

	t.Run("update existing plan", func(t *testing.T) {
		plan := &config.BackupSchedule{
			ID:         "plan-1",
			Name:       "Updated Plan 1",
			Enabled:    false,
			BackupType: "table",
		}

		err := store.SavePlan(ctx, plan)
		require.NoError(t, err)

		plans, err := store.ListPlans(ctx)
		require.NoError(t, err)

		var found *config.BackupSchedule
		for _, p := range plans {
			if p.ID == "plan-1" {
				found = p
				break
			}
		}

		require.NotNil(t, found)
		assert.Equal(t, "Updated Plan 1", found.Name)
		assert.Equal(t, "table", found.BackupType)
	})

	t.Run("delete plan", func(t *testing.T) {
		err := store.DeletePlan(ctx, "plan-1")
		require.NoError(t, err)

		plans, err := store.ListPlans(ctx)
		assert.NoError(t, err)
		assert.Len(t, plans, 1)
		assert.Equal(t, "plan-2", plans[0].ID)
	})

	t.Run("delete non-existent plan", func(t *testing.T) {
		err := store.DeletePlan(ctx, "non-existent")
		assert.NoError(t, err)
	})
}

func TestRedisPlanStore_Executions_Fallback(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()
	planID := "test-plan"

	now := time.Now()
	exec1 := &BackupExecution{
		ID:        "exec-1",
		PlanID:    planID,
		Status:    ExecutionStatusCompleted,
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   ptrTime(now.Add(-30 * time.Minute)),
	}

	exec2 := &BackupExecution{
		ID:        "exec-2",
		PlanID:    planID,
		Status:    ExecutionStatusRunning,
		StartTime: now,
	}

	t.Run("empty executions initially", func(t *testing.T) {
		executions, err := store.ListExecutions(ctx, planID, 10)
		assert.NoError(t, err)
		assert.Empty(t, executions)
	})

	t.Run("save and list executions", func(t *testing.T) {
		err := store.SaveExecution(ctx, exec1)
		require.NoError(t, err)

		err = store.SaveExecution(ctx, exec2)
		require.NoError(t, err)

		executions, err := store.ListExecutions(ctx, planID, 10)
		assert.NoError(t, err)
		assert.Len(t, executions, 2)

		assert.Equal(t, "exec-2", executions[0].ID)
		assert.Equal(t, "exec-1", executions[1].ID)
	})

	t.Run("limit executions", func(t *testing.T) {
		executions, err := store.ListExecutions(ctx, planID, 1)
		assert.NoError(t, err)
		assert.Len(t, executions, 1)
		assert.Equal(t, "exec-2", executions[0].ID)
	})

	t.Run("executions for non-existent plan", func(t *testing.T) {
		executions, err := store.ListExecutions(ctx, "non-existent-plan", 10)
		assert.NoError(t, err)
		assert.Empty(t, executions)
	})
}

func TestRedisPlanStore_Validation(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()

	t.Run("save plan with empty ID", func(t *testing.T) {
		err := store.SavePlan(ctx, &config.BackupSchedule{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ID is required")
	})

	t.Run("save nil plan", func(t *testing.T) {
		err := store.SavePlan(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is nil")
	})

	t.Run("save execution with empty ID", func(t *testing.T) {
		err := store.SaveExecution(ctx, &BackupExecution{PlanID: "plan-1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "execution ID is required")
	})

	t.Run("save execution with empty PlanID", func(t *testing.T) {
		err := store.SaveExecution(ctx, &BackupExecution{ID: "exec-1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "execution PlanID is required")
	})

	t.Run("save nil execution", func(t *testing.T) {
		err := store.SaveExecution(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "execution is nil")
	})
}

func TestRedisPlanStore_Timestamps(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()

	t.Run("CreatedAt preserved on update", func(t *testing.T) {
		originalTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		plan := &config.BackupSchedule{
			ID:         "ts-plan",
			Name:       "Timestamp Test",
			Enabled:    true,
			BackupType: "metadata",
			CreatedAt:  originalTime,
		}

		err := store.SavePlan(ctx, plan)
		require.NoError(t, err)
		assert.Equal(t, originalTime, plan.CreatedAt)

		time.Sleep(10 * time.Millisecond)

		plan.Name = "Updated Name"
		err = store.SavePlan(ctx, plan)
		require.NoError(t, err)
		assert.Equal(t, originalTime, plan.CreatedAt)
		assert.True(t, plan.UpdatedAt.After(originalTime))
	})

	t.Run("Execution CreatedAt preserved on update", func(t *testing.T) {
		originalTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		exec := &BackupExecution{
			ID:        "ts-exec",
			PlanID:    "ts-plan",
			Status:    ExecutionStatusPending,
			StartTime: originalTime,
			CreatedAt: originalTime,
		}

		err := store.SaveExecution(ctx, exec)
		require.NoError(t, err)
		assert.Equal(t, originalTime, exec.CreatedAt)

		time.Sleep(10 * time.Millisecond)

		exec.Status = ExecutionStatusCompleted
		err = store.SaveExecution(ctx, exec)
		require.NoError(t, err)
		assert.Equal(t, originalTime, exec.CreatedAt)
		assert.True(t, exec.UpdatedAt.After(originalTime))
	})
}

func TestRedisPlanStore_DeletePlanCascades(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()

	plan := &config.BackupSchedule{
		ID:         "cascade-plan",
		Name:       "Cascade Test",
		Enabled:    true,
		BackupType: "metadata",
	}

	exec := &BackupExecution{
		ID:        "cascade-exec",
		PlanID:    "cascade-plan",
		Status:    ExecutionStatusCompleted,
		StartTime: time.Now(),
	}

	err := store.SavePlan(ctx, plan)
	require.NoError(t, err)

	err = store.SaveExecution(ctx, exec)
	require.NoError(t, err)

	err = store.DeletePlan(ctx, "cascade-plan")
	require.NoError(t, err)

	plans, err := store.ListPlans(ctx)
	require.NoError(t, err)
	assert.Empty(t, plans)

	executions, err := store.ListExecutions(ctx, "cascade-plan", 10)
	require.NoError(t, err)
	assert.Empty(t, executions)
}

func TestPrependExecution(t *testing.T) {
	exec1 := &BackupExecution{ID: "exec-1"}
	exec2 := &BackupExecution{ID: "exec-2"}
	exec3 := &BackupExecution{ID: "exec-3"}

	slice := []*BackupExecution{exec1, exec2}
	result := prependExecution(slice, exec3)

	assert.Len(t, result, 3)
	assert.Equal(t, "exec-3", result[0].ID)
	assert.Equal(t, "exec-1", result[1].ID)
	assert.Equal(t, "exec-2", result[2].ID)

	assert.Len(t, slice, 2, "original slice should not be modified")
}

func TestRedisPlanStore_DefaultLimit(t *testing.T) {
	store := NewRedisPlanStore(nil, nil)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		exec := &BackupExecution{
			ID:        string(rune('a' + i)),
			PlanID:    "limit-plan",
			Status:    ExecutionStatusCompleted,
			StartTime: time.Now(),
		}
		err := store.SaveExecution(ctx, exec)
		require.NoError(t, err)
	}

	executions, err := store.ListExecutions(ctx, "limit-plan", 0)
	require.NoError(t, err)
	assert.Len(t, executions, 5)

	executions, err = store.ListExecutions(ctx, "limit-plan", -1)
	require.NoError(t, err)
	assert.Len(t, executions, 5)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
