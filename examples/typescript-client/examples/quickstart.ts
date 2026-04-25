/**
 * HotPlex Gateway - Quick Start Example
 * 
 * Minimal demo showing how to connect to the gateway and send a simple task.
 * 
 * Usage:
 *   npx tsx examples/quickstart.ts
 * 
 * Prerequisites:
 *   - HotPlex Gateway running at ws://localhost:8888/ws
 *   - Claude Code CLI installed and accessible
 */

import { HotPlexClient, WorkerType } from '../src/index.js';
import { generateTestToken } from '../scripts/generate-test-token.js';

async function main() {
  console.log('🚀 HotPlex Gateway - Quick Start\n');

  const token = await generateTestToken();
  
  // Create client connecting to local gateway
  const client = new HotPlexClient({
    url: 'ws://localhost:8888/ws',
    workerType: WorkerType.ClaudeCode,
    apiKey: 'dev-api-key',
    authToken: token,
  });

  // Handle streaming output
  client.on('delta', (data) => {
    process.stdout.write(data.content);
  });

  // Handle completion
  client.on('done', (data) => {
    console.log('\n\n✅ Task completed:', data.success);
    if (data.stats) {
      console.log(`   Duration: ${data.stats.duration_ms}ms`);
      console.log(`   Tokens: ${data.stats.total_tokens}`);
      console.log(`   Cost: $${data.stats.cost_usd}`);
    }
    client.disconnect();
  });

  // Handle errors
  client.on('error', (data) => {
    console.error('\n❌ Error:', data.code, '-', data.message);
    client.disconnect();
    process.exit(1);
  });

  try {
    // Connect (creates new session)
    console.log('Connecting to gateway...');
    const ack = await client.connect();
    console.log(`Connected! Session: ${ack.session_id}\n`);
    console.log('Sending task to Claude Code...\n');

    // Send a simple task
    client.sendInput('Write a hello world program in Go that prints "Hello, World!" to stdout.');

  } catch (err) {
    console.error('Failed to connect:', err);
    process.exit(1);
  }
}

main();