#!/usr/bin/env node

/**
 * MinIODB Node.js TypeScript SDK gRPC 代码生成脚本
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const SCRIPT_DIR = __dirname;
const PROJECT_ROOT = path.dirname(SCRIPT_DIR);
const PROTO_DIR = path.join(PROJECT_ROOT, '..', 'proto');
const OUTPUT_DIR = path.join(PROJECT_ROOT, 'src', 'proto');

console.log('=== MinIODB Node.js gRPC 代码生成 ===');
console.log('项目根目录:', PROJECT_ROOT);
console.log('Proto 目录:', PROTO_DIR);
console.log('输出目录:', OUTPUT_DIR);

// 检查必要的工具
function checkDependencies() {
    console.log('检查依赖...');
    
    try {
        execSync('npx grpc_tools_node_protoc --version', { stdio: 'ignore' });
        console.log('✅ grpc_tools_node_protoc 可用');
    } catch (error) {
        console.error('❌ grpc_tools_node_protoc 未安装');
        console.error('请运行: npm install grpc-tools');
        process.exit(1);
    }

    try {
        execSync('npx grpc_tools_node_protoc_ts --version', { stdio: 'ignore' });
        console.log('✅ grpc_tools_node_protoc_ts 可用');
    } catch (error) {
        console.error('❌ grpc_tools_node_protoc_ts 未安装');
        console.error('请运行: npm install grpc_tools_node_protoc_ts');
        process.exit(1);
    }
}

// 检查 proto 文件是否存在
function checkProtoFiles() {
    const protoFile = path.join(PROTO_DIR, 'miniodb', 'v1', 'miniodb.proto');
    
    if (!fs.existsSync(protoFile)) {
        console.error('❌ Proto 文件不存在:', protoFile);
        process.exit(1);
    }
    
    console.log('✅ Proto 文件存在');
}

// 创建输出目录
function createOutputDir() {
    if (!fs.existsSync(OUTPUT_DIR)) {
        fs.mkdirSync(OUTPUT_DIR, { recursive: true });
        console.log('✅ 创建输出目录:', OUTPUT_DIR);
    }
}

// 生成 JavaScript gRPC 代码
function generateJavaScriptCode() {
    console.log('正在生成 JavaScript gRPC 代码...');
    
    const command = [
        'npx grpc_tools_node_protoc',
        `--proto_path=${PROTO_DIR}`,
        `--js_out=import_style=commonjs,binary:${OUTPUT_DIR}`,
        `--grpc_out=grpc_js:${OUTPUT_DIR}`,
        `${PROTO_DIR}/miniodb/v1/miniodb.proto`
    ].join(' ');
    
    try {
        execSync(command, { stdio: 'inherit' });
        console.log('✅ JavaScript gRPC 代码生成成功');
    } catch (error) {
        console.error('❌ JavaScript gRPC 代码生成失败');
        process.exit(1);
    }
}

// 生成 TypeScript 类型定义
function generateTypeScriptDefinitions() {
    console.log('正在生成 TypeScript 类型定义...');
    
    const command = [
        'npx grpc_tools_node_protoc_ts',
        `--proto_path=${PROTO_DIR}`,
        `--ts_out=grpc_js:${OUTPUT_DIR}`,
        `${PROTO_DIR}/miniodb/v1/miniodb.proto`
    ].join(' ');
    
    try {
        execSync(command, { stdio: 'inherit' });
        console.log('✅ TypeScript 类型定义生成成功');
    } catch (error) {
        console.error('❌ TypeScript 类型定义生成失败');
        process.exit(1);
    }
}

// 创建索引文件
function createIndexFile() {
    const indexContent = `/**
 * MinIODB gRPC 生成代码索引文件
 * 
 * 此文件导出生成的 gRPC 客户端和类型定义。
 */

// 导出生成的 gRPC 代码
export * from './miniodb/v1/miniodb_pb';
export * from './miniodb/v1/miniodb_grpc_pb';

// 注意：实际的文件名可能根据生成工具而有所不同
// 请根据实际生成的文件调整导出路径
`;

    const indexFile = path.join(OUTPUT_DIR, 'index.ts');
    fs.writeFileSync(indexFile, indexContent);
    console.log('✅ 创建索引文件:', indexFile);
}

// 更新 package.json 脚本
function updatePackageScripts() {
    const packagePath = path.join(PROJECT_ROOT, 'package.json');
    
    if (fs.existsSync(packagePath)) {
        const packageJson = JSON.parse(fs.readFileSync(packagePath, 'utf8'));
        
        if (!packageJson.scripts) {
            packageJson.scripts = {};
        }
        
        packageJson.scripts['generate:proto'] = 'node scripts/generate-proto.js';
        
        fs.writeFileSync(packagePath, JSON.stringify(packageJson, null, 2));
        console.log('✅ 更新 package.json 脚本');
    }
}

// 主函数
function main() {
    try {
        checkDependencies();
        checkProtoFiles();
        createOutputDir();
        generateJavaScriptCode();
        generateTypeScriptDefinitions();
        createIndexFile();
        updatePackageScripts();
        
        console.log('\n=== Node.js gRPC 代码生成完成 ===');
        console.log('生成的文件位于:', OUTPUT_DIR);
        console.log('\n下一步:');
        console.log('1. 运行 npm run build 编译 TypeScript 代码');
        console.log('2. 在客户端代码中使用生成的 gRPC 类型');
        console.log('3. 实现完整的客户端功能');
    } catch (error) {
        console.error('❌ 代码生成失败:', error.message);
        process.exit(1);
    }
}

// 运行脚本
if (require.main === module) {
    main();
}
