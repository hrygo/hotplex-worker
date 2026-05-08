/**
 * Custom error classes for HotPlex Client
 */

export class HotPlexError extends Error {
  constructor(message: string, public readonly code?: string, public readonly details?: Record<string, unknown>) {
    super(message);
    this.name = 'HotPlexError';
    Object.setPrototypeOf(this, HotPlexError.prototype);
  }
}

export class ConnectionError extends HotPlexError {
  constructor(message: string, details?: Record<string, unknown>) {
    super(message, 'CONNECTION_ERROR', details);
    this.name = 'ConnectionError';
    Object.setPrototypeOf(this, ConnectionError.prototype);
  }
}

export class SessionError extends HotPlexError {
  constructor(message: string, code: string, details?: Record<string, unknown>) {
    super(message, code, details);
    this.name = 'SessionError';
    Object.setPrototypeOf(this, SessionError.prototype);
  }
}

export class TimeoutError extends HotPlexError {
  constructor(message: string) {
    super(message, 'TIMEOUT');
    this.name = 'TimeoutError';
    Object.setPrototypeOf(this, TimeoutError.prototype);
  }
}

export class ProtocolError extends HotPlexError {
  constructor(message: string, code: string, details?: Record<string, unknown>) {
    super(message, code, details);
    this.name = 'ProtocolError';
    Object.setPrototypeOf(this, ProtocolError.prototype);
  }
}
