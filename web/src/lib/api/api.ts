// HTTP client for the Conductor REST API. Endpoints in SPEC §18.3.
// Implementation lands in Phase 15; this file is a typed surface for now.

export type IssueOrchestrationState =
  | 'unclaimed'
  | 'classifying'
  | 'claimed'
  | 'running'
  | 'validating'
  | 'retry_queued'
  | 'enforcer_blocked'
  | 'released';

export type Severity = 'info' | 'warning' | 'error' | 'blocking';
export type EnforcerStatus = 'clear' | 'violations_present' | 'blocked';
export type KnowledgeIndexStatus = 'ready' | 'indexing' | 'stale' | 'disabled';

export interface Issue {
  id: string;
  identifier: string;
  title: string;
  description: string | null;
  state: string;
  priority: number | null;
  labels: string[];
  task_type: string | null;
  estimated_complexity: 'trivial' | 'small' | 'medium' | 'large' | 'epic' | null;
}

export interface RuntimeStatus {
  enforcer_status: EnforcerStatus;
  knowledge_index_status: KnowledgeIndexStatus;
  pending_gc_tasks: number;
  running: number;
  max_concurrent_agents: number;
}

export interface AuditEvent {
  id: string;
  timestamp: string;
  project_id: string;
  issue_id: string | null;
  session_id: string | null;
  agent_role: string | null;
  event_type: string;
  payload: Record<string, unknown>;
  parent_event_id: string | null;
  duration_ms: number | null;
}

export interface ValidationResult {
  check_id: string;
  status: 'passed' | 'failed' | 'timeout';
  severity: Severity;
  exit_code: number;
  output: string;
  duration_ms: number;
}

export interface MemoryEntry {
  id: string;
  layer: 'episodic' | 'semantic' | 'procedural';
  project_id: string;
  issue_id: string | null;
  task_type: string | null;
  content: string;
  tags: string[];
  source: 'agent_written' | 'auto_extracted' | 'consolidated' | 'validation_result';
  created_at: string;
  expires_at: string | null;
}

export interface ApiClient {
  status(): Promise<RuntimeStatus>;
  listIssues(): Promise<Issue[]>;
  getIssue(id: string): Promise<Issue>;
  dispatchIssue(id: string): Promise<void>;
  cancelIssue(id: string): Promise<void>;
  listAudit(filters?: {
    issue_id?: string;
    role?: string;
    type?: string;
    cursor?: string;
  }): Promise<{ events: AuditEvent[]; next_cursor: string | null }>;
  listMemories(projectId: string, issueId?: string): Promise<MemoryEntry[]>;
  retireMemory(id: string): Promise<void>;
  consolidateMemories(): Promise<void>;
  searchKnowledge(query: string): Promise<unknown>;
  searchDocs(query: string): Promise<unknown>;
  getHarness(): Promise<{ raw: string; parsed: unknown }>;
  putHarness(raw: string): Promise<void>;
  listHarnessViolations(): Promise<unknown>;
  runHarnessCheck(): Promise<unknown>;
  listWorkspaces(): Promise<unknown>;
  deleteWorkspace(key: string): Promise<void>;
  listProviders(): Promise<unknown>;
}

export function createApiClient(_baseUrl = '/api/v1'): ApiClient {
  // Implementation lands in Phase 15.
  throw new Error('createApiClient: not yet implemented (see docs/phases.md → Phase 15)');
}
