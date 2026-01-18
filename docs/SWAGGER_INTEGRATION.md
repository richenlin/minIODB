# MinIODB Swagger文档集成方案

> 创建日期: 2026-01-18
> 状态: 待实施

## 概述

为MinIODB项目集成Swagger/OpenAPI文档，实现：
- 在线API文档浏览（访问 `/api-docs/index.html`）
- 支持在线调试调用接口
- JWT Bearer Token认证支持
- 生产环境可配置禁用

## 技术选型

| 组件 | 版本 | 用途 |
|------|------|------|
| `github.com/swaggo/swag` | latest | Swagger文档生成CLI |
| `github.com/swaggo/gin-swagger` | latest | Gin框架Swagger中间件 |
| `github.com/swaggo/files` | latest | Swagger UI静态文件 |

## 文件修改清单

### 新增文件
| 文件 | 说明 |
|------|------|
| `docs/docs.go` | Swagger初始化代码（自动生成） |
| `docs/swagger.json` | OpenAPI 3.0规范JSON（自动生成） |
| `docs/swagger.yaml` | OpenAPI 3.0规范YAML（自动生成） |

### 修改文件
| 文件 | 修改内容 |
|------|----------|
| `go.mod` | 添加3个Swagger依赖 |
| `config/config.go` | 添加`SwaggerConfig`结构体 |
| `config.yaml` | 添加`swagger`配置节 |
| `cmd/main.go` | 添加Swagger全局注释（@title, @version等） |
| `internal/transport/rest/server.go` | 添加18个Handler的Swagger注释 + Swagger路由 |

---

## 详细实施步骤

### 步骤1: 添加依赖

**文件**: `go.mod`

```bash
go get -u github.com/swaggo/swag/cmd/swag
go get -u github.com/swaggo/gin-swagger
go get -u github.com/swaggo/files
```

---

### 步骤2: 添加配置结构体

**文件**: `config/config.go`

```go
// SwaggerConfig Swagger文档配置
type SwaggerConfig struct {
    Enabled     bool   `yaml:"enabled"`      // 是否启用Swagger UI
    Host        string `yaml:"host"`         // API主机地址
    BasePath    string `yaml:"base_path"`    // API基础路径
    Title       string `yaml:"title"`        // API标题
    Description string `yaml:"description"`  // API描述
}
```

在`Config`结构体中添加：
```go
type Config struct {
    // ... 现有字段
    Swagger           SwaggerConfig           `yaml:"swagger"`           // Swagger配置
}
```

---

### 步骤3: 添加配置

**文件**: `config.yaml`

```yaml
# Swagger API文档配置
swagger:
  enabled: true                              # 生产环境设为false
  host: "localhost:8081"                     # API主机地址
  base_path: "/v1"                           # API基础路径
  title: "MinIODB API"                       # API标题
  description: "基于MinIO+DuckDB+Redis的分布式OLAP系统API"
```

---

### 步骤4: 添加全局注释

**文件**: `cmd/main.go` (在package声明前添加)

```go
// @title           MinIODB API
// @version         1.0
// @description     基于MinIO+DuckDB+Redis的分布式OLAP系统API
// @description     支持高性能数据写入、SQL查询、表管理、元数据备份等功能

// @contact.name   MinIODB Support
// @contact.url    https://github.com/richenlin/minIODB

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8081
// @BasePath  /v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer Token认证，格式: Bearer {token}

// @tag.name 认证
// @tag.description 用户认证和Token管理

// @tag.name 数据操作
// @tag.description 数据写入、查询、更新、删除

// @tag.name 表管理
// @tag.description 表的创建、查询、删除

// @tag.name 元数据
// @tag.description 元数据备份、恢复、状态查询

// @tag.name 系统监控
// @tag.description 健康检查、状态查询、性能指标

package main
```

---

### 步骤5: 添加Handler注释

**文件**: `internal/transport/rest/server.go`

需要为以下18个接口添加Swagger注释：

#### 5.1 认证接口 (3个)

```go
// getToken 获取JWT令牌
// @Summary      获取JWT令牌
// @Description  使用API Key和Secret获取访问令牌
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request body object{api_key=string,api_secret=string} true "认证请求"
// @Success      200 {object} object{access_token=string,refresh_token=string,expires_in=int,token_type=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Router       /auth/token [post]
func (s *Server) getToken(c *gin.Context) {

// refreshToken 刷新JWT令牌
// @Summary      刷新JWT令牌
// @Description  使用Refresh Token获取新的访问令牌
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request body object{refresh_token=string} true "刷新请求"
// @Success      200 {object} object{access_token=string,refresh_token=string,expires_in=int,token_type=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Router       /auth/refresh [post]
func (s *Server) refreshToken(c *gin.Context) {

// revokeToken 撤销JWT令牌
// @Summary      撤销JWT令牌
// @Description  撤销指定的访问令牌
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request body object{token=string} true "撤销请求"
// @Success      200 {object} object{success=bool,message=string}
// @Failure      400 {object} ErrorResponse
// @Router       /auth/token [delete]
func (s *Server) revokeToken(c *gin.Context) {
```

#### 5.2 系统接口 (1个)

```go
// healthCheck 健康检查
// @Summary      健康检查
// @Description  检查服务健康状态
// @Tags         系统监控
// @Produce      json
// @Success      200 {object} HealthResponse
// @Failure      500 {object} ErrorResponse
// @Router       /health [get]
func (s *Server) healthCheck(c *gin.Context) {
```

#### 5.3 数据操作接口 (4个)

```go
// writeData 写入数据
// @Summary      写入数据
// @Description  向指定表写入一条数据记录
// @Tags         数据操作
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body WriteRequest true "写入请求"
// @Success      200 {object} WriteResponse
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /data [post]
func (s *Server) writeData(c *gin.Context) {

// queryData 查询数据
// @Summary      查询数据
// @Description  执行SQL查询
// @Tags         数据操作
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body QueryRequest true "查询请求"
// @Success      200 {object} QueryResponse
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /query [post]
func (s *Server) queryData(c *gin.Context) {

// updateData 更新数据
// @Summary      更新数据
// @Description  更新指定表中的数据记录
// @Tags         数据操作
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body object{table=string,id=string,payload=object,partial=bool} true "更新请求"
// @Success      200 {object} object{success=bool,message=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /data [put]
func (s *Server) updateData(c *gin.Context) {

// deleteData 删除数据
// @Summary      删除数据
// @Description  删除指定表中的数据记录
// @Tags         数据操作
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body object{table=string,ids=[]string} true "删除请求"
// @Success      200 {object} object{success=bool,message=string,deleted_count=int,errors=[]string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /data [delete]
func (s *Server) deleteData(c *gin.Context) {
```

#### 5.4 表管理接口 (4个)

```go
// createTable 创建表
// @Summary      创建表
// @Description  创建新的数据表
// @Tags         表管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body CreateTableRequest true "创建表请求"
// @Success      200 {object} CreateTableResponse
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /tables [post]
func (s *Server) createTable(c *gin.Context) {

// listTables 列出表
// @Summary      列出表
// @Description  获取所有表的列表
// @Tags         表管理
// @Produce      json
// @Security     BearerAuth
// @Param        pattern query string false "表名匹配模式"
// @Success      200 {object} object{tables=[]string}
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /tables [get]
func (s *Server) listTables(c *gin.Context) {

// getTable 获取表信息
// @Summary      获取表信息
// @Description  获取指定表的详细信息
// @Tags         表管理
// @Produce      json
// @Security     BearerAuth
// @Param        name path string true "表名"
// @Success      200 {object} object{table_name=string,config=TableConfig,created_at=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /tables/{name} [get]
func (s *Server) getTable(c *gin.Context) {

// deleteTable 删除表
// @Summary      删除表
// @Description  删除指定的数据表
// @Tags         表管理
// @Produce      json
// @Security     BearerAuth
// @Param        name path string true "表名"
// @Param        if_exists query bool false "仅当存在时删除"
// @Param        cascade query bool false "级联删除相关数据"
// @Success      200 {object} object{success=bool,message=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /tables/{name} [delete]
func (s *Server) deleteTable(c *gin.Context) {
```

#### 5.5 元数据接口 (4个)

```go
// backupMetadata 备份元数据
// @Summary      备份元数据
// @Description  备份系统元数据到MinIO
// @Tags         元数据
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body object{backup_name=string,description=string} true "备份请求"
// @Success      200 {object} object{success=bool,message=string,backup_id=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /metadata/backup [post]
func (s *Server) backupMetadata(c *gin.Context) {

// restoreMetadata 恢复元数据
// @Summary      恢复元数据
// @Description  从备份恢复系统元数据
// @Tags         元数据
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body object{backup_id=string} true "恢复请求"
// @Success      200 {object} object{success=bool,message=string}
// @Failure      400 {object} ErrorResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /metadata/restore [post]
func (s *Server) restoreMetadata(c *gin.Context) {

// listBackups 列出备份
// @Summary      列出备份
// @Description  获取元数据备份列表
// @Tags         元数据
// @Produce      json
// @Security     BearerAuth
// @Param        days query int false "查询天数范围" default(30)
// @Success      200 {object} object{backups=[]object{id=string,name=string,created_at=string}}
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /metadata/backups [get]
func (s *Server) listBackups(c *gin.Context) {

// getMetadataStatus 获取元数据状态
// @Summary      获取元数据状态
// @Description  获取系统元数据的当前状态
// @Tags         元数据
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} object{status=string,last_backup=string,tables_count=int}
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /metadata/status [get]
func (s *Server) getMetadataStatus(c *gin.Context) {
```

#### 5.6 监控接口 (2个)

```go
// getStatus 获取系统状态
// @Summary      获取系统状态
// @Description  获取系统运行状态和统计信息
// @Tags         系统监控
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} StatsResponse
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /status [get]
func (s *Server) getStatus(c *gin.Context) {

// getMetrics 获取性能指标
// @Summary      获取性能指标
// @Description  获取系统性能监控指标
// @Tags         系统监控
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} object{timestamp=string,performance_metrics=object,resource_usage=object}
// @Failure      401 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /metrics [get]
func (s *Server) getMetrics(c *gin.Context) {
```

---

### 步骤6: 添加Swagger路由

**文件**: `internal/transport/rest/server.go`

#### 6.1 添加导入

```go
import (
    // ... 现有导入
    swaggerFiles "github.com/swaggo/files"
    ginSwagger "github.com/swaggo/gin-swagger"
    _ "minIODB/docs"  // 导入生成的Swagger文档
)
```

#### 6.2 修改setupRoutes函数

在`setupRoutes`函数开头添加：

```go
func (s *Server) setupRoutes() {
    // Swagger API文档路由（根据配置决定是否启用）
    if s.cfg.Swagger.Enabled {
        s.router.GET("/api-docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
            ginSwagger.URL("/api-docs/doc.json"),
            ginSwagger.DefaultModelsExpandDepth(-1),
        ))
        logger.GetLogger().Sugar().Info("Swagger UI enabled at /api-docs/index.html")
    }
    
    // ... 其他路由配置
}
```

---

### 步骤7: 生成文档

```bash
# 安装swag CLI（如未安装）
go install github.com/swaggo/swag/cmd/swag@latest

# 在项目根目录生成文档
swag init -g cmd/main.go -o docs --parseDependency --parseInternal

# 验证生成的文件
ls -la docs/
# 应该看到: docs.go, swagger.json, swagger.yaml
```

---

### 步骤8: 测试和推送

```bash
# 编译验证
go build -o /tmp/miniodb_test ./cmd/main.go

# 启动服务（测试环境）
./miniodb_test

# 访问Swagger UI
open http://localhost:8081/api-docs/index.html

# 提交代码
git add .
git commit -m "feat: 集成Swagger API文档"
git push origin main
```

---

## 接口清单统计

| 分组 | 接口数 | 需要认证 |
|------|--------|----------|
| 认证 | 3 | 否 |
| 系统监控 | 3 | 部分 |
| 数据操作 | 4 | 是 |
| 表管理 | 4 | 是 |
| 元数据 | 4 | 是 |
| **总计** | **18** | - |

---

## 配置说明

### 开发环境 (config.yaml)
```yaml
swagger:
  enabled: true
  host: "localhost:8081"
```

### 生产环境 (config.prod.yaml)
```yaml
swagger:
  enabled: false
```

---

## 认证说明

Swagger UI中使用JWT认证的步骤：

1. 点击页面右上角 **"Authorize"** 按钮
2. 在输入框中填入: `Bearer <your_jwt_token>`
3. 点击 **"Authorize"** 确认
4. 之后所有需要认证的接口都会自动带上Authorization头

---

## 预期效果

启动服务后访问 `http://localhost:8081/api-docs/index.html`:

- 左侧显示所有API分组（认证、数据操作、表管理、元数据、系统监控）
- 点击每个接口可以展开详细说明
- 可以直接填写参数并点击 **"Try it out"** 在线调试
- 支持导出OpenAPI规范文件

---

## 预估工作量

| 任务 | 预估时间 |
|------|----------|
| 添加依赖 | 5分钟 |
| 添加配置结构体和配置 | 10分钟 |
| 添加全局注释 | 10分钟 |
| 为18个接口添加注释 | 60分钟 |
| 添加Swagger路由 | 5分钟 |
| 生成文档并测试 | 10分钟 |
| **总计** | **约90分钟** |
