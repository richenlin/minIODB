# 测试脚本目录

本目录包含MinIODB项目的各种测试和诊断脚本。

## 脚本说明

### 1. test_buffer_fix.sh
**用途**: 验证缓冲区数据实时查询修复效果
**功能**: 
- 创建测试表
- 写入测试数据
- 验证实时查询功能
- 测试手动刷新功能

**使用方法**:
```bash
cd test/scripts
./test_buffer_fix.sh
```

**前置条件**: MinIODB服务必须运行在localhost:8080

### 2. fix_go_env.sh
**用途**: 修复Go编译环境问题
**功能**:
- 检查Go环境配置
- 修复GOROOT和GOPATH
- 清理并重新下载依赖
- 编译性能测试工具

**使用方法**:
```bash
cd test/scripts
./fix_go_env.sh
```

**适用场景**: 当遇到"GOROOT路径配置不正确"等编译错误时使用

### 3. establish_baseline.sh
**用途**: 建立系统性能基线
**功能**:
- 自动执行写入性能测试
- 执行各种查询性能测试
- 生成性能基线报告

**使用方法**:
```bash
cd test/scripts
./establish_baseline.sh
```

**输出**: 生成`baseline_report.md`性能基线报告

**前置条件**: MinIODB服务必须运行在localhost:8080

## 执行权限

确保脚本具有执行权限：
```bash
chmod +x *.sh
```

## 注意事项

1. 所有脚本都假设MinIODB服务运行在`http://localhost:8080`
2. 建议在测试环境中运行，避免影响生产数据
3. 运行脚本前请确保Docker服务正常运行
4. 某些脚本可能需要额外的工具如`jq`、`bc`等

## 故障排除

如果脚本执行失败，请检查：
1. MinIODB服务是否正常运行
2. 网络连接是否正常
3. 是否有足够的磁盘空间
4. 相关依赖工具是否已安装

## 相关文档

- [性能测试说明](../performance/README.md)
- [项目主README](../../README.md)
- [性能报告](../../PERFORMANCE_REPORT.md) 