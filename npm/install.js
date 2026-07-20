#!/usr/bin/env node
const os = require('os');
const fs = require('fs');
const path = require('path');
const https = require('https');
const { execSync } = require('child_process');

const VERSION = '0.3.0';
const BASE_URL = `https://github.com/bounty/bounty/releases/download/v${VERSION}`;

const platform = os.platform(); // darwin, linux, win32
const arch = os.arch();         // x64, arm64

const map = {
  'darwin-x64': 'bounty-darwin-amd64',
  'darwin-arm64': 'bounty-darwin-arm64',
  'linux-x64': 'bounty-linux-amd64',
  'linux-arm64': 'bounty-linux-arm64',
  'win32-x64': 'bounty-windows-amd64.exe',
};

const binaryName = map[`${platform}-${arch}`];
if (!binaryName) {
  console.error(`Unsupported platform: ${platform}-${arch}`);
  process.exit(1);
}

const ext = platform === 'win32' ? '.exe' : '';
const url = `${BASE_URL}/${binaryName}${platform === 'win32' ? '' : '.tar.gz'}`;
const binDir = path.join(__dirname, 'bin');
const binPath = path.join(binDir, `bounty${ext}`);

if (!fs.existsSync(binDir)) {
  fs.mkdirSync(binDir, { recursive: true });
}

console.log(`Downloading Bounty ${VERSION} for ${platform}-${arch}...`);

// For tarball:
if (platform !== 'win32') {
  const tar = require('tar');
  const stream = https.get(url, (res) => {
    if (res.statusCode === 302 || res.statusCode === 301) {
      https.get(res.headers.location, (redirectRes) => {
        redirectRes.pipe(tar.x({ C: binDir, strip: 1 }));
      });
      return;
    }
    res.pipe(tar.x({ C: binDir, strip: 1 }));
  });
} else {
  // Windows: direct exe download
  const file = fs.createWriteStream(binPath);
  https.get(url, (res) => { res.pipe(file); });
}

// Make executable
if (platform !== 'win32') {
  setTimeout(() => {
    try { fs.chmodSync(binPath, 0o755); } catch (e) {}
  }, 3000);
}

console.log('Bounty installed! Run: npx bounty chat');
