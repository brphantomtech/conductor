// WebSocket client for the Conductor real-time channel. Protocol in SPEC §18.2.
// Implementation lands in Phase 15; this file is a typed surface for now.

import type { AuditEvent, RuntimeStatus, ValidationResult } from './api';

export type ServerMessage =
  | { type: 'OrchestratorState'; payload: RuntimeStatus }
  | { type: 'AuditEvent'; payload: AuditEvent }
  | { type: 'AgentTurnStream'; payload: { issue_id: string; chunk: string } }
  | { type: 'ValidationResult'; payload: ValidationResult };

export type ClientMessage = { type: 'Subscribe'; issue_ids: string[] };

export interface WsClient {
  connect(url?: string): Promise<void>;
  subscribe(issueIds: string[]): void;
  on<T extends ServerMessage['type']>(
    type: T,
    handler: (payload: Extract<ServerMessage, { type: T }>['payload']) => void
  ): () => void;
  close(): void;
}

export function createWsClient(): WsClient {
  // Implementation lands in Phase 15.
  throw new Error('createWsClient: not yet implemented (see docs/phases.md → Phase 15)');
}
