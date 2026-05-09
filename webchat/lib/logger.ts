/**
 * Structured logger for webchat.
 *
 * Replaces raw console calls with JSON-formatted entries containing
 * module, sessionId, and timestamp. Future-ready for Sentry/error
 * tracking endpoint integration.
 */

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface LogEntry {
  level: LogLevel;
  module: string;
  message: string;
  sessionId?: string;
  data?: Record<string, unknown>;
  timestamp: number;
}

function emit(entry: LogEntry): void {
  const args: unknown[] = [JSON.stringify(entry)];
  switch (entry.level) {
    case 'error': console.error(...args); break;
    case 'warn':  console.warn(...args);  break;
    case 'info':  console.info(...args);  break;
    default:      console.log(...args);   break;
  }
}

function createLog(level: LogLevel, module: string, message: string, data?: Record<string, unknown>) {
  emit({ level, module, message, data, timestamp: Date.now() });
}

export const logger = {
  debug:   (m: string, msg: string, d?: Record<string, unknown>) => createLog('debug', m, msg, d),
  info:    (m: string, msg: string, d?: Record<string, unknown>) => createLog('info', m, msg, d),
  warn:    (m: string, msg: string, d?: Record<string, unknown>) => createLog('warn', m, msg, d),
  error:   (m: string, msg: string, d?: Record<string, unknown>) => createLog('error', m, msg, d),
};
