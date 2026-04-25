/**
 * HotPlex Gateway - Complete Example
 * 
 * Full-featured demo showing all client capabilities:
 * - Custom configuration (model, system prompt, tools)
 * - Streaming output with typing indicator
 * - Tool call monitoring
 * - Permission request handling
 * - Session resume
 * - Error recovery
 * - Stats display
 * - Graceful shutdown
 * 
 * Usage:
 *   npx tsx examples/complete.ts
 */

import * as readline from 'readline';
import { HotPlexClient, WorkerType, SessionState, ErrorCode } from '../src/index.js';
import { generateTestToken } from '../scripts/generate-test-token.js';

// ============================================================================
// Types
// ============================================================================

interface DemoConfig {
  url: string;
  sessionId?: string;
  model?: string;
  systemPrompt?: string;
  allowedTools?: string[];
  workDir?: string;
}

// ============================================================================
// Configuration
// ============================================================================

const CONFIG: DemoConfig = {
  url: process.env.HOTPLEX_URL || 'ws://localhost:8888',
  sessionId: process.env.HOTPLEX_SESSION_ID, // Resume existing session if set
  model: 'claude-sonnet-4-6',
  systemPrompt: 'You are a helpful coding assistant. Be concise and informative.',
  allowedTools: ['read_file', 'write_file', 'bash', 'grep', 'glob'],
  workDir: process.cwd(),
};

// ============================================================================
// Helpers
// ============================================================================

function createTypingIndicator(): { stop: () => void } {
  const frames = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
  let i = 0;
  let interval: NodeJS.Timeout;

  const stop = () => {
    clearInterval(interval);
    process.stdout.write('\r' + ' '.repeat(10) + '\r');
  };

  interval = setInterval(() => {
    process.stdout.write(`\r${frames[i++ % frames.length]} Processing...`);
  }, 80);

  return { stop };
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

// Safe tool names that don't require user confirmation
const AUTO_APPROVE_TOOLS = ['read_file', 'grep', 'glob', 'bash'];

// ============================================================================
// Main Demo
// ============================================================================

async function main() {
  console.log('═══════════════════════════════════════════════════════════════');
  console.log('      HotPlex Gateway - Complete Example');
  console.log('═══════════════════════════════════════════════════════════════\n');

  printConfig();

  const token = await generateTestToken();
  
  // Create client
  const client = new HotPlexClient({
    url: CONFIG.url + '/ws',
    workerType: WorkerType.ClaudeCode,
    apiKey: 'dev-api-key',
    authToken: token,
    reconnect: {
      enabled: true,
      maxAttempts: 5,
      baseDelayMs: 1000,
      maxDelayMs: 30000,
    },
  });

  // State
  let sessionId: string | null = null;
  const typingIndicator = createTypingIndicator();

  // ============================================================================
  // Event Handlers
  // ============================================================================

  client.on('connected', (ack) => {
    typingIndicator.stop();
    sessionId = ack.session_id;
    console.log('\n✅ Connected to gateway');
    console.log(`   Session ID: ${sessionId}`);
    console.log(`   Worker: ${ack.server_caps.worker_type}`);
    console.log(`   State: ${ack.state}`);
    console.log(`   Supports Resume: ${ack.server_caps.supports_resume}`);
    console.log('\n─────────────────────────────────────────────────────────────\n');
  });

  client.on('disconnected', (reason) => {
    typingIndicator.stop();
    console.log('\n📴 Disconnected:', reason);
  });

  client.on('reconnecting', (attempt) => {
    console.log(`\n🔄 Reconnecting (attempt ${attempt})...`);
  });

  client.on('state', (data) => {
    console.log(`\n📊 State changed: ${data.state}`);
  });

  client.on('delta', (data) => {
    process.stdout.write(data.content);
  });

  client.on('message', (data) => {
    typingIndicator.stop();
    console.log('\n📨 Full message received:');
    console.log(`   Role: ${data.role}`);
    console.log(`   Content: ${data.content.substring(0, 100)}${data.content.length > 100 ? '...' : ''}`);
  });

  client.on('toolCall', (data) => {
    typingIndicator.stop();
    console.log('\n🔧 Tool called:');
    console.log(`   ID: ${data.id}`);
    console.log(`   Tool: ${data.name}`);
    console.log(`   Args: ${JSON.stringify(data.input)}`);
  });

  client.on('toolResult', (data) => {
    console.log('📋 Tool result:');
    console.log(`   ID: ${data.id}`);
    if (data.error) {
      console.log(`   ❌ Error: ${data.error}`);
    } else {
      const output = typeof data.output === 'string' 
        ? data.output.substring(0, 100) + (data.output.length > 100 ? '...' : '')
        : JSON.stringify(data.output);
      console.log(`   Output: ${output}`);
    }
  });

  client.on('reasoning', (data) => {
    // Optional: display thinking process
    // console.log('\n💭 Thinking:', data.content.substring(0, 50) + '...');
  });

  client.on('done', (data) => {
    typingIndicator.stop();
    console.log('\n\n═══════════════════════════════════════════════════════════════');
    console.log('                        TASK COMPLETED');
    console.log('═══════════════════════════════════════════════════════════════');
    console.log(`\n   Success: ${data.success ? '✅' : '❌'}`);
    
    if (data.stats) {
      console.log('\n   📈 Statistics:');
      console.log(`      Duration:    ${formatDuration(data.stats.duration_ms || 0)}`);
      console.log(`      Tool Calls:  ${data.stats.tool_calls || 0}`);
      console.log(`      Input Tokens:  ${data.stats.input_tokens?.toLocaleString() || 'N/A'}`);
      console.log(`      Output Tokens: ${data.stats.output_tokens?.toLocaleString() || 'N/A'}`);
      if (data.stats.cache_read_tokens) {
        console.log(`      Cache Hits:   ${data.stats.cache_read_tokens?.toLocaleString()}`);
      }
      console.log(`      Total Tokens: ${data.stats.total_tokens?.toLocaleString() || 'N/A'}`);
      if (data.stats.cost_usd) {
        console.log(`      Cost:         $${data.stats.cost_usd.toFixed(4)}`);
      }
      if (data.stats.model) {
        console.log(`      Model:        ${data.stats.model}`);
      }
    }

    console.log(`\n   💾 Session ID for resume: ${sessionId}`);
    console.log('\n   Run with: HOTPLEX_SESSION_ID=' + sessionId + ' npx tsx examples/complete.ts');
    console.log('═══════════════════════════════════════════════════════════════\n');

    client.disconnect();
  });

  client.on('error', (data) => {
    typingIndicator.stop();
    console.error('\n\n❌ ERROR:');
    console.error(`   Code: ${data.code}`);
    console.error(`   Message: ${data.message}`);
    
    if (data.code === ErrorCode.SessionBusy) {
      console.error('   Note: Session is busy, will auto-retry...');
      return;
    }
    
    if (data.code === ErrorCode.Unauthorized) {
      console.error('   Note: Authentication required. Set HOTPLEX_AUTH_TOKEN environment variable.');
    }

    if (data.details) {
      console.error('   Details:', JSON.stringify(data.details, null, 2));
    }
  });

  client.on('permissionRequest', (data) => {
    typingIndicator.stop();
    console.log('\n\n🔐 PERMISSION REQUEST:');
    console.log(`   Tool: ${data.tool_name}`);
    console.log(`   Description: ${data.description || 'N/A'}`);
    
    // Auto-approve safe tools
    if (AUTO_APPROVE_TOOLS.includes(data.tool_name)) {
      console.log('   → Auto-approving (safe tool)');
      client.sendPermissionResponse(data.id, true);
    } else {
      console.log('   → Denying (potentially unsafe tool)');
      client.sendPermissionResponse(data.id, false, 'Tool not in auto-approve list');
    }
  });

  client.on('throttle', (data) => {
    console.log('\n⚠️  Throttled by server');
    if (data.suggestion) {
      console.log(`   Max rate: ${data.suggestion.max_message_rate}`);
      console.log(`   Backoff: ${data.suggestion.backoff_ms}ms`);
    }
  });

  client.on('reconnect', (data) => {
    console.log('\n🔄 Server requested reconnect');
    console.log(`   Reason: ${data.reason}`);
  });

  client.on('sessionInvalid', (data) => {
    console.log('\n🚫 Session invalidated');
    console.log(`   Reason: ${data.reason}`);
    console.log(`   Recoverable: ${data.recoverable}`);
  });

  // ============================================================================
  // Graceful Shutdown
  // ============================================================================

  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  const shutdown = (signal: string) => {
    console.log(`\n\nReceived ${signal}. Shutting down gracefully...`);
    typingIndicator.stop();
    
    // Option 1: Just disconnect
    // client.disconnect();
    
    // Option 2: Terminate session on server
    if (sessionId) {
      console.log('Terminating session on server...');
      client.sendControl('terminate');
    }
    
    setTimeout(() => {
      client.disconnect();
      rl.close();
      process.exit(0);
    }, 1000);
  };

  process.on('SIGINT', () => shutdown('SIGINT'));
  process.on('SIGTERM', () => shutdown('SIGTERM'));

  // ============================================================================
  // Connect and Run
  // ============================================================================

  try {
    console.log('Connecting to gateway...');
    
    if (CONFIG.sessionId) {
      console.log(`Resuming session: ${CONFIG.sessionId}`);
      await client.resume(CONFIG.sessionId);
    } else {
      await client.connect();
    }

    // Demo task
    const task = process.env.HOTPLEX_TASK || 
      'Create a simple HTTP server in Go that handles GET /health returning 200 OK with JSON body {"status":"ok"}. Include proper error handling and a main function that starts the server on port 8080.';

    console.log('\n📤 Sending task...\n');
    client.sendInput(task);

  } catch (err) {
    console.error('\n❌ Failed to connect:', err);
    process.exit(1);
  }
}

// ============================================================================
// Helper Functions
// ============================================================================

function printConfig() {
  console.log('Configuration:');
  console.log(`   Gateway URL: ${CONFIG.url}`);
  if (CONFIG.sessionId) {
    console.log(`   Session ID: ${CONFIG.sessionId} (RESUME MODE)`);
  }
  if (CONFIG.model) {
    console.log(`   Model: ${CONFIG.model}`);
  }
  if (CONFIG.systemPrompt) {
    console.log(`   System Prompt: ${CONFIG.systemPrompt.substring(0, 50)}...`);
  }
  if (CONFIG.allowedTools) {
    console.log(`   Allowed Tools: ${CONFIG.allowedTools.join(', ')}`);
  }
  console.log('');
}

main();