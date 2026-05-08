/**
 * Jittered Exponential Backoff implementation
 */

export interface BackoffConfig {
  baseDelayMs: number;
  maxDelayMs: number;
  jitter?: number; // 0 to 1, default 0.1
}

export function calculateBackoff(attempt: number, config: BackoffConfig): number {
  if (attempt <= 0) return 0;
  
  const { baseDelayMs, maxDelayMs, jitter = 0.1 } = config;
  
  // Exponential backoff: base * 2^(attempt-1)
  let delay = baseDelayMs * Math.pow(2, attempt - 1);
  
  // Cap at maxDelay
  delay = Math.min(delay, maxDelayMs);
  
  // Add jitter
  if (jitter > 0) {
    const jitterAmount = delay * jitter;
    const randomJitter = (Math.random() * 2 - 1) * jitterAmount;
    delay += randomJitter;
  }
  
  return Math.max(0, delay);
}
