package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"minIODB/config"
	"minIODB/pkg/pool"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

var planIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)

func validatePlanID(id string) error {
	if id == "" {
		return errors.New("plan id cannot be empty")
	}
	if len(id) > 64 {
		return errors.New("plan id too long (max 64 chars)")
	}
	if !planIDRegex.MatchString(id) {
		return errors.New("plan id must contain only alphanumeric characters and hyphens")
	}
	return nil
}

type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"
	ExecutionStatusRunning   ExecutionStatus = "running"
	ExecutionStatusCompleted ExecutionStatus = "completed"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

type BackupExecution struct {
	ID        string          `json:"id"`
	PlanID    string          `json:"plan_id"`
	Status    ExecutionStatus `json:"status"`
	StartTime time.Time       `json:"start_time"`
	EndTime   *time.Time      `json:"end_time,omitempty"`
	Error     string          `json:"error,omitempty"`
	Manifest  string          `json:"manifest,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type PlanStore interface {
	ListPlans(ctx context.Context) ([]*config.BackupSchedule, error)
	SavePlan(ctx context.Context, plan *config.BackupSchedule) error
	DeletePlan(ctx context.Context, id string) error
	ListExecutions(ctx context.Context, planID string, limit int) ([]*BackupExecution, error)
	SaveExecution(ctx context.Context, exec *BackupExecution) error
}

type RedisPlanStore struct {
	redisPool *pool.RedisPool
	keyPrefix string
	logger    *zap.Logger

	fallbackStore *FallbackStore
	mu            sync.RWMutex
}

type FallbackStore struct {
	plans      map[string]*config.BackupSchedule
	executions map[string][]*BackupExecution
	mu         sync.RWMutex
}

func NewFallbackStore() *FallbackStore {
	return &FallbackStore{
		plans:      make(map[string]*config.BackupSchedule),
		executions: make(map[string][]*BackupExecution),
	}
}

func NewRedisPlanStore(redisPool *pool.RedisPool, logger *zap.Logger) *RedisPlanStore {
	return &RedisPlanStore{
		redisPool:     redisPool,
		keyPrefix:     "miniodb:backup",
		logger:        logger,
		fallbackStore: NewFallbackStore(),
	}
}

func (s *RedisPlanStore) planKey(id string) string {
	return fmt.Sprintf("%s:plan:%s", s.keyPrefix, id)
}

func (s *RedisPlanStore) plansListKey() string {
	return fmt.Sprintf("%s:plans", s.keyPrefix)
}

func (s *RedisPlanStore) executionsKey(planID string) string {
	return fmt.Sprintf("%s:executions:%s", s.keyPrefix, planID)
}

func (s *RedisPlanStore) isRedisAvailable(ctx context.Context) bool {
	if s.redisPool == nil {
		return false
	}
	client := s.redisPool.GetClient()
	if client == nil {
		return false
	}
	err := client.Ping(ctx).Err()
	return err == nil
}

func (s *RedisPlanStore) ListPlans(ctx context.Context) ([]*config.BackupSchedule, error) {
	if !s.isRedisAvailable(ctx) {
		return s.listPlansFallback()
	}

	client := s.redisPool.GetClient()
	planIDs, err := client.LRange(ctx, s.plansListKey(), 0, -1).Result()
	if err == redis.Nil {
		return []*config.BackupSchedule{}, nil
	}
	if err != nil {
		s.logger.Warn("Redis list plans failed, using fallback", zap.Error(err))
		return s.listPlansFallback()
	}

	plans := make([]*config.BackupSchedule, 0, len(planIDs))
	for _, planID := range planIDs {
		plan, err := s.getPlanByID(ctx, planID)
		if err != nil {
			s.logger.Warn("Failed to get plan, skipping",
				zap.String("plan_id", planID),
				zap.Error(err))
			continue
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (s *RedisPlanStore) listPlansFallback() ([]*config.BackupSchedule, error) {
	s.fallbackStore.mu.RLock()
	defer s.fallbackStore.mu.RUnlock()

	plans := make([]*config.BackupSchedule, 0, len(s.fallbackStore.plans))
	for _, plan := range s.fallbackStore.plans {
		plans = append(plans, plan)
	}
	return plans, nil
}

func (s *RedisPlanStore) getPlanByID(ctx context.Context, id string) (*config.BackupSchedule, error) {
	if err := validatePlanID(id); err != nil {
		return nil, err
	}
	if !s.isRedisAvailable(ctx) {
		return s.getPlanByIDFallback(id)
	}
	client := s.redisPool.GetClient()
	data, err := client.Get(ctx, s.planKey(id)).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("plan not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get plan from redis: %w", err)
	}

	var plan config.BackupSchedule
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}

	return &plan, nil
}

func (s *RedisPlanStore) getPlanByIDFallback(id string) (*config.BackupSchedule, error) {
	s.fallbackStore.mu.RLock()
	defer s.fallbackStore.mu.RUnlock()

	plan, ok := s.fallbackStore.plans[id]
	if !ok {
		return nil, fmt.Errorf("plan not found: %s", id)
	}
	return plan, nil
}

func (s *RedisPlanStore) SavePlan(ctx context.Context, plan *config.BackupSchedule) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if err := validatePlanID(plan.ID); err != nil {
		return err
	}

	now := time.Now()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now

	if !s.isRedisAvailable(ctx) {
		return s.savePlanFallback(plan)
	}

	client := s.redisPool.GetClient()
	data, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	pipe := client.TxPipeline()
	pipe.Set(ctx, s.planKey(plan.ID), data, 0)
	pipe.LRem(ctx, s.plansListKey(), 0, plan.ID)
	pipe.LPush(ctx, s.plansListKey(), plan.ID)

	if _, err := pipe.Exec(ctx); err != nil {
		s.logger.Warn("Redis save plan failed, using fallback", zap.Error(err))
		return s.savePlanFallback(plan)
	}

	return nil
}

func (s *RedisPlanStore) savePlanFallback(plan *config.BackupSchedule) error {
	s.fallbackStore.mu.Lock()
	defer s.fallbackStore.mu.Unlock()

	s.fallbackStore.plans[plan.ID] = plan
	return nil
}

func (s *RedisPlanStore) DeletePlan(ctx context.Context, id string) error {
	if err := validatePlanID(id); err != nil {
		return err
	}
	if !s.isRedisAvailable(ctx) {
		return s.deletePlanFallback(id)
	}

	client := s.redisPool.GetClient()
	pipe := client.TxPipeline()
	pipe.Del(ctx, s.planKey(id))
	pipe.LRem(ctx, s.plansListKey(), 0, id)
	pipe.Del(ctx, s.executionsKey(id))

	if _, err := pipe.Exec(ctx); err != nil {
		s.logger.Warn("Redis delete plan failed, using fallback", zap.Error(err))
		return s.deletePlanFallback(id)
	}

	return nil
}

func (s *RedisPlanStore) deletePlanFallback(id string) error {
	s.fallbackStore.mu.Lock()
	defer s.fallbackStore.mu.Unlock()

	delete(s.fallbackStore.plans, id)
	delete(s.fallbackStore.executions, id)
	return nil
}

func (s *RedisPlanStore) ListExecutions(ctx context.Context, planID string, limit int) ([]*BackupExecution, error) {
	if err := validatePlanID(planID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	if !s.isRedisAvailable(ctx) {
		return s.listExecutionsFallback(planID, limit)
	}

	client := s.redisPool.GetClient()
	results, err := client.LRange(ctx, s.executionsKey(planID), 0, int64(limit-1)).Result()
	if err == redis.Nil {
		return []*BackupExecution{}, nil
	}
	if err != nil {
		s.logger.Warn("Redis list executions failed, using fallback", zap.Error(err))
		return s.listExecutionsFallback(planID, limit)
	}

	executions := make([]*BackupExecution, 0, len(results))
	for _, result := range results {
		var exec BackupExecution
		if err := json.Unmarshal([]byte(result), &exec); err != nil {
			s.logger.Warn("Failed to unmarshal execution, skipping",
				zap.String("plan_id", planID),
				zap.Error(err))
			continue
		}
		executions = append(executions, &exec)
	}

	return executions, nil
}

func (s *RedisPlanStore) listExecutionsFallback(planID string, limit int) ([]*BackupExecution, error) {
	s.fallbackStore.mu.RLock()
	defer s.fallbackStore.mu.RUnlock()

	executions, ok := s.fallbackStore.executions[planID]
	if !ok {
		return []*BackupExecution{}, nil
	}

	if limit > len(executions) {
		limit = len(executions)
	}

	result := make([]*BackupExecution, limit)
	for i := 0; i < limit; i++ {
		result[i] = executions[i]
	}
	return result, nil
}

func (s *RedisPlanStore) SaveExecution(ctx context.Context, exec *BackupExecution) error {
	if exec == nil {
		return fmt.Errorf("execution is nil")
	}
	if exec.ID == "" {
		return fmt.Errorf("execution ID is required")
	}
	if err := validatePlanID(exec.PlanID); err != nil {
		return err
	}

	now := time.Now()
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = now
	}
	exec.UpdatedAt = now

	if !s.isRedisAvailable(ctx) {
		return s.saveExecutionFallback(exec)
	}

	client := s.redisPool.GetClient()
	data, err := json.Marshal(exec)
	if err != nil {
		return fmt.Errorf("marshal execution: %w", err)
	}

	key := s.executionsKey(exec.PlanID)
	pipe := client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, 999)

	if _, err := pipe.Exec(ctx); err != nil {
		s.logger.Warn("Redis save execution failed, using fallback", zap.Error(err))
		return s.saveExecutionFallback(exec)
	}

	return nil
}

func (s *RedisPlanStore) saveExecutionFallback(exec *BackupExecution) error {
	s.fallbackStore.mu.Lock()
	defer s.fallbackStore.mu.Unlock()

	executions, ok := s.fallbackStore.executions[exec.PlanID]
	if !ok {
		executions = make([]*BackupExecution, 0)
	}

	result := prependExecution(executions, exec)
	if len(result) > 1000 {
		result = result[:1000]
	}
	s.fallbackStore.executions[exec.PlanID] = result
	return nil
}

func prependExecution(slice []*BackupExecution, item *BackupExecution) []*BackupExecution {
	result := make([]*BackupExecution, len(slice)+1)
	result[0] = item
	copy(result[1:], slice)
	return result
}
