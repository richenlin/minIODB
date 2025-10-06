/**
 * Jest 测试设置文件
 */

// 设置测试超时
jest.setTimeout(30000);

// 全局测试配置
beforeAll(async () => {
  // 测试前的全局设置
});

afterAll(async () => {
  // 测试后的全局清理
});

// 模拟 console 方法以避免测试输出干扰
global.console = {
  ...console,
  // 保留错误和警告
  error: jest.fn(),
  warn: jest.fn(),
  // 静默日志和信息
  log: jest.fn(),
  info: jest.fn(),
  debug: jest.fn(),
};
