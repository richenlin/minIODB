# MinIODB 详细优化计划

本文档详细说明了 MinIODB 项目的逐步优化计划。每个任务都被设计为一个独立的、可测试的单元，以便于工程 LLM 逐一执行。

## 使用说明

每个任务均满足：准确精简、可测试、单一聚焦、明确开始与结束，并按实现顺序排列。每条包含“修改点/产出”和“验收标准”。

---

## 阶段A：可用性与一致性

1. 提供简化配置样例与加载支持
   - 修改点/产出：新增 `config.simple.yaml`；`internal/config/config.go` 支持 simple 覆盖；`README.md` 增补快速上手段落。
   - 验收标准：使用 simple 启动成功；缺省项走默认；单测覆盖缺失/非法字段提示。

2. 核心路径结构化日志统一
   - 修改点/产出：`internal/service/miniodb_service.go`、`internal/query/query.go` 全量用 `internal/logger`（zap）替代 `log.Printf`，补充字段（table/id/sql/duration/status）。
   - 验收标准：无 `log.Printf` 残留；关键操作输出 JSON；性能波动 <5%。

3. 错误处理一致化（服务层）
   - 修改点/产出：服务层错误统一“记录+返回”，由 HTTP/gRPC 层映射错误码。
   - 验收标准：失败路径返回 4xx/5xx 且有单条结构化错误日志。

4. 将硬编码 TTL 配置化
   - 修改点/产出：把 `metadataCacheTTL` 与 `localFileIndexTTL` 接入配置（保留 5m 默认）。
   - 验收标准：单测验证命中/过期在不同 TTL 下可控。

---

## 阶段B：查询可靠性与可控性

5. 查询超时与上下文传递
   - 修改点/产出：新增 `QueryTimeout`；`ExecuteQuery` 用 `context.WithTimeout` 贯穿 DB/MinIO/索引访问。
   - 验收标准：慢查询命中超时；无泄漏（race 通过）。

6. 慢查询阈值与计数
   - 修改点/产出：新增 `SlowQueryThreshold`；超阈值记录结构化日志并增加 `miniodb_slow_queries_total`。
   - 验收标准：>阈值产生指标+日志，<=阈值不产生。

7. 查询耗时直方图与分位
   - 修改点/产出：自定义分桶；在查询路径 `Observe`；Grafana 面板示例。
   - 验收标准：单测不同耗时命中分桶；P50/P95/P99 可出图。

---

## 阶段C：缓存一致性与失效

8. 表级文件索引缓存主动失效（Redis 可选）
   - 修改点/产出：写入/刷新后发布 `table.invalidate.{table}`；`Querier` 订阅并清理本地文件索引与查询缓存；单机模式本地失效。
   - 验收标准：写后无需等待 TTL 即可见新数据；订阅断线自动重连。

9. 查询缓存 TTL 自适应（轻接入）
   - 修改点/产出：对接 `smart_cache`，根据模式/频率调整 TTL，受 `MinTTL/MaxTTL` 约束。
   - 验收标准：高频拉长、低频缩短，TTL 始终在区间内。

---

## 阶段D：写入/缓冲稳定性

10. 基于内存压力的自适应刷新
    - 修改点/产出：`concurrent_buffer.checkAdaptiveFlush` 增加内存占用检测与阈值（配置化）。
    - 验收标准：内存高于阈值触发刷新；正常不触发。

11. 刷新/上传超时与重试配置化
    - 修改点/产出：将 `FlushTimeout/MaxRetries/RetryDelay` 接入全局/表级配置并生效于上传与索引更新。
    - 验收标准：单测注入失败，重试次数与延迟符合配置。

---

## 阶段E：元数据备份/恢复

12. 备份校验和与元信息
    - 修改点/产出：生成快照 JSON 的 SHA-256（对象元数据或 `.checksum`）；恢复前校验。
    - 验收标准：篡改导致校验失败；未篡改通过。

13. 恢复并行化（受控、幂等）
    - 修改点/产出：`RecoveryOptions.Parallel` 启用 Worker Pool；按 100 条/批管道提交，批间 Flush。
    - 验收标准：相同数据并行回复吞吐≥2×；顺序/并行结果一致。

14. 启动一致性检查闭环
    - 修改点/产出：完善 `performStartupSync` 路径的校验/快照/恢复/验证/版本更新与失败回滚提示。
    - 验收标准：构造 `redis_newer/backup_newer/versions_equal` 三状态，行为与日志匹配设计。

---

## 阶段F：元数据原子性与正确性

15. `.metadata` 原子创建（单机保证）
    - 修改点/产出：创建前 `Stat` 与表名粒度锁；可用条件写（If-None-Match）优先。
    - 验收标准：并发压测下仅 1 个 `.metadata`，其余返回已存在。

16. 表元数据缓存主动失效 API
    - 修改点/产出：新增内部 API `POST /v1/tables/{name}/invalidate`（运维模式），触发表级缓存清理。
    - 验收标准：调用后缓存命中率下降并重新加载成功。

---

## 阶段G：监控与告警

17. 写入与缓冲指标补强
    - 修改点/产出：写入与刷新路径计数与耗时直方图；提供 Grafana 面板片段。
    - 验收标准：`/metrics` 可抓取；面板可加载。

18. 连接池耗尽与尾部时延告警
    - 修改点/产出：Prometheus 规则：Redis/MinIO 活跃率>90%与查询 P99 超阈值报警；README 指南。
    - 验收标准：压测可触发与恢复。

---

## 阶段H：文档与测试

19. 导出符号注释补全
    - 修改点/产出：为 `internal/metadata`、`internal/query`、`internal/buffer` 导出符号补 GoDoc。
    - 验收标准：lint 无导出未注释告警（新增处）。

20. 覆盖率提升到 70%+
    - 修改点/产出：新增用例：配置加载/校验、查询超时、缓存失效（含 Pub/Sub）、备份校验和、恢复并行、缓冲自适应刷新。
    - 验收标准：`go test ./... -cover` 全局≥70%，核心包≥75%。

21. 故障排查文档补全
    - 修改点/产出：`docs/TROUBLESHOOTING.md` 增补错误码、慢查询定位、恢复失败（校验和/并行）。
    - 验收标准：文档指向具体指标与日志字段。

---

## 阶段I：可选增强

22. 配置向导 CLI
    - 修改点/产出：新增 `cmd/configgen`，问答式生成 `config.simple.yaml` 与表级模板。
    - 验收标准：向导生成配置可直接启动服务。

23. REST/gRPC 限制与超时保护
    - 修改点/产出：基于 `config.Network.Server` 落地 REST/gRPC 读/写/头超时与最大头/包大小；拒绝超限请求。
    - 验收标准：超限请求 4xx；正常不受影响。

---

## CI 与交付要求（跨任务通用）

- 引入/更新检查：
  - 覆盖率门禁 ≥ 70%。
  - `promtool check rules` 与 lint 通过。
- 每个任务提交包含：代码变更、单测/脚本、必要的文档片段。
