#!/usr/bin/env node

const childProcess = require('child_process');

const BINARY_MAP = {
  darwin_x64: {name: 'redis-mcp-server-darwin-amd64', suffix: ''},
  darwin_arm64: {name: 'redis-mcp-server-darwin-arm64', suffix: ''},
  linux_x64: {name: 'redis-mcp-server-linux-amd64', suffix: ''},
  linux_arm64: {name: 'redis-mcp-server-linux-arm64', suffix: ''},
  win32_x64: {name: 'redis-mcp-server-windows-amd64', suffix: '.exe'},
};

const resolveBinaryPath = () => {
  const binary = BINARY_MAP[`${process.platform}_${process.arch}`];
  if (!binary) {
    throw new Error(`Unsupported platform/architecture: ${process.platform}/${process.arch}`);
  }

  try {
    return require.resolve(`@futuretea/${binary.name}/bin/redis-mcp-server${binary.suffix}`);
  } catch {
    throw new Error(`Could not resolve binary path for platform/architecture: ${process.platform}/${process.arch}`);
  }
};

const child = childProcess.spawn(resolveBinaryPath(), ['mcp', ...process.argv.slice(2)], {
  stdio: 'inherit',
});

const handleSignal = (signal) => {
  if (!child.killed) {
    child.kill(signal);
  }
};

['SIGTERM', 'SIGINT', 'SIGHUP'].forEach((signal) => {
  process.on(signal, () => handleSignal(signal));
});

child.on('close', (code, signal) => {
  if (signal) {
    process.exit(128 + (signal === 'SIGTERM' ? 15 : signal === 'SIGINT' ? 2 : 1));
  }

  process.exit(code || 0);
});
