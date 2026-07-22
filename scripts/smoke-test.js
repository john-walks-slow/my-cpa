#!/usr/bin/env node
// Browser smoke test for compare-share dashboard.
// Serves the dashboard files locally and verifies basic UI functionality.

const http = require('http');
const fs = require('fs');
const path = require('path');

const DIST = path.join(__dirname, '..', 'plugin', 'dashboard', 'web', 'dist');
const PORT = 8765;

const MIME = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
};

// Mock API responses
const mockData = {
  keys: ['openai|gpt-4|gpt-4|auth-1', 'openai|gpt-3.5|gpt-3.5|auth-1'],
  compare: {
    schema_version: 1,
    generated_at: new Date().toISOString(),
    title: 'Model comparison',
    range: { preset: '24h', from: '', to: '' },
    metric: 'p95_ttft_ms',
    subjects: [
      { kind: 'model', id: 'gpt-4', label: 'gpt-4' },
      { kind: 'model', id: 'gpt-3.5', label: 'gpt-3.5' },
    ],
    rows: [
      {
        subject: 'gpt-4',
        label: 'gpt-4',
        count: 100,
        success_rate: 0.98,
        p95_ttft_ms: 250,
        ttft_observed: 100,
        avg_stream_rate_tps: 45.2,
        avg_latency_ms: 1200,
        trend_24h_over_24h: -0.05,
        series: [
          { at: new Date(Date.now() - 3600000).toISOString(), value: 240, raw_ttft_ms: 240 },
          { at: new Date().toISOString(), value: 250, raw_ttft_ms: 250 },
        ],
      },
      {
        subject: 'gpt-3.5',
        label: 'gpt-3.5',
        count: 200,
        success_rate: 0.99,
        p95_ttft_ms: 120,
        ttft_observed: 200,
        avg_stream_rate_tps: 85.5,
        avg_latency_ms: 600,
        trend_24h_over_24h: 0.02,
        series: [
          { at: new Date(Date.now() - 3600000).toISOString(), value: 115, raw_ttft_ms: 115 },
          { at: new Date().toISOString(), value: 120, raw_ttft_ms: 120 },
        ],
      },
    ],
  },
};

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`);
  
  // API endpoints
  if (url.pathname === '/v0/management/stats/keys') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(mockData.keys));
    return;
  }
  if (url.pathname === '/v0/management/stats/compare') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(mockData.compare));
    return;
  }
  if (url.pathname === '/v0/resource/plugins/my-cpa-stats-plugin/share-data') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(mockData.compare));
    return;
  }
  
  // Static files
  let filePath = url.pathname;
  if (filePath === '/' || filePath === '/index.html') {
    filePath = '/index.html';
  }
  
  const fullPath = path.join(DIST, filePath);
  const ext = path.extname(fullPath);
  
  fs.readFile(fullPath, (err, data) => {
    if (err) {
      res.writeHead(404);
      res.end('Not found');
      return;
    }
    res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
    res.end(data);
  });
});

server.listen(PORT, () => {
  console.log(`Dashboard smoke test server running at http://localhost:${PORT}/index.html`);
  console.log('Open this URL in a browser to verify:');
  console.log('  1. Dashboard loads without console errors');
  console.log('  2. Click "Compare" button');
  console.log('  3. Select 2 models from the picker');
  console.log('  4. Verify KPI cards, chart, and ranking table render');
  console.log('  5. Hover over chart to see tooltip with all series');
  console.log('  6. Click CSV button to download export');
  console.log('  7. Test share.html?id=test for read-only mode');
  console.log('\nPress Ctrl+C to stop the server.');
});
