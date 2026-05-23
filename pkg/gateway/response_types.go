package gateway

// ResponseType defines the business-level semantic classification of a message
// pushed from server to client. This is an application-layer concept, distinct
// from the protocol-layer Command (BEGN/TEXT/JSON/OK/ERR/CMD) and from
// ContentType (text/plain, application/json, etc. MIME types).
//
// When the server sends a structured response via Server.SendResponse(), the
// Message.Data field contains a JSON-encoded ResponseEnvelope with one of these
// type values. The client uses this type to dispatch to a registered handler.
type ResponseType string

const (
	// --- Built-in response types ---

	// RespText indicates a plain-text notification or message.
	RespText ResponseType = "text"

	// RespTable indicates tabular data that should be rendered as a table.
	// Data should be: {"headers": [...], "rows": [[...], ...]}
	RespTable ResponseType = "table"

	// RespTodo indicates a todo/checklist item list.
	// Data should be: [{"id": ..., "text": ..., "done": ...}, ...]
	RespTodo ResponseType = "todo"

	// RespOptions indicates a selectable options list (single or multi-select).
	// Data should be: [{"value": ..., "label": ...}, ...]
	RespOptions ResponseType = "options"

	// RespForm indicates a form input request asking the user to fill fields.
	// Data should be: {"fields": [{"name", "label", "type", "required", ...}]}
	RespForm ResponseType = "form"

	// RespProgress indicates a progress update for a long-running operation.
	// Data should be: {"total": N, "current": N, "message": "..."}
	RespProgress ResponseType = "progress"

	// RespError indicates a business-level error that should be displayed to the user.
	// Data should be: {"code": "...", "message": "..."} or a plain string
	RespError ResponseType = "error"

	// RespConfirm indicates a confirmation request requiring user yes/no action.
	// Data should be: {"message": "...", "detail": "..."}
	RespConfirm ResponseType = "confirm"

	// RespFile indicates binary or file data being transferred.
	// Data should be: {"filename": "...", "mime": "...", "size": N, "data": "base64..."}
	RespFile ResponseType = "file"

	// RespMarkdown indicates rich text in Markdown format that should be
	// rendered with terminal styling (colored headings, bold, italic, lists,
	// code blocks, etc.). Data should be a raw Markdown string.
	RespMarkdown ResponseType = "markdown"

	// --- Agent (GoReact) event response types ---

	// RespThinkingDelta indicates a streaming fragment of the agent's thinking process.
	// Data should be a plain text string (the thinking fragment).
	RespThinkingDelta ResponseType = "thinking_delta"

	// RespThinkingDone indicates the agent completed a thinking cycle.
	// Data should be: {"reasoning": "...", "decision": "...", "confidence": 0.0}
	RespThinkingDone ResponseType = "thinking_done"

	// RespActionStart indicates the agent is about to execute a tool.
	// Data should be: {"tool_name": "...", "params": {...}}
	RespActionStart ResponseType = "action_start"

	// RespActionProgress indicates intermediate progress of a long-running tool.
	// Data should be a plain text string (progress description).
	RespActionProgress ResponseType = "action_progress"

	// RespActionResult indicates a tool execution completed.
	// Data should be: {"tool_name": "...", "result": "...", "error": "...", "duration_ms": N, "success": true}
	RespActionResult ResponseType = "action_result"

	// RespActionEnd indicates the entire action (all tool calls) has completed.
	// Data should be: {"total": N, "success": N, "failed": N, "summary": "..."}
	RespActionEnd ResponseType = "action_end"

	// RespSubtaskSpawned indicates a subagent task has been created.
	// Data should be: {"task_id": "...", "agent_name": "...", "description": "..."}
	RespSubtaskSpawned ResponseType = "subtask_spawned"

	// RespSubtaskCompleted indicates a subagent task has finished.
	// Data should be: {"task_id": "...", "success": true, "answer": "...", "error": "..."}
	RespSubtaskCompleted ResponseType = "subtask_completed"

	// RespFinalAnswer indicates the agent produced its final answer.
	// Data should be a plain text string (the final answer, possibly markdown).
	RespFinalAnswer ResponseType = "final_answer"

	// RespClarifyNeeded indicates the agent needs user clarification.
	// Data should be a plain text string (the question).
	RespClarifyNeeded ResponseType = "clarify_needed"

	// RespPermissionRequest indicates the agent needs user authorization.
	// Data should be: {"tool_name": "...", "reason": "...", "security_level": "..."}
	RespPermissionRequest ResponseType = "permission_request"

	// RespPermissionDenied indicates a tool execution was denied.
	// Data should be a plain text string (denial reason).
	RespPermissionDenied ResponseType = "permission_denied"

	// RespExecutionSummary indicates the agent execution completed with stats.
	// Data should be: {"total_iterations": N, "tool_calls": N, "tools_used": [...], "total_duration_ms": N, "tokens_used": N}
	RespExecutionSummary ResponseType = "execution_summary"

	// RespCycleEnd indicates one T-A-O cycle has ended.
	// Data should be: {"iteration": N, "duration_ms": N, "termination_reason": "..."}
	RespCycleEnd ResponseType = "cycle_end"

	// RespTaskSummary indicates a natural-language summary of the completed task.
	// Data should be: {"summary": "...", "input_tokens": N, "output_tokens": N}
	RespTaskSummary ResponseType = "task_summary"

	// --- Session & Memory RPC response types ---

	// RespSessionList indicates the response contains a list of sessions.
	// Data should be: []SessionInfo
	RespSessionList ResponseType = "session_list"

	// RespSessionInfo indicates detailed information about a single session.
	// Data should be: SessionInfo
	RespSessionInfo ResponseType = "session_info"

	// RespMemoryResult indicates memory query/search results.
	// Data should be: []MemoryRecord
	RespMemoryResult ResponseType = "memory_result"

	// RespMemoryStored indicates a memory record was successfully stored.
	// Data should be: {"id": "..."}
	RespMemoryStored ResponseType = "memory_stored"

	// --- Agent/Model/Skill RPC response types ---

	// RespAgentList indicates the response contains a list of agents.
	// Data should be: []AgentConfig
	RespAgentList ResponseType = "agent_list"

	// RespAgentUpdated indicates an agent config was successfully updated.
	// Data should be: {"status": "ok", "agent_name": "..."}
	RespAgentUpdated ResponseType = "agent_updated"

	// RespModelList indicates the response contains a list of models.
	// Data should be: []ModelConfig
	RespModelList ResponseType = "model_list"

	// RespSkillList indicates the response contains a list of skills.
	// Data should be: []Skill
	RespSkillList ResponseType = "skill_list"

	// --- Compatibility ---

	// RespUnknown is the zero value used when no type is specified (backward compat).
	RespUnknown ResponseType = ""
)

// ResponseEnvelope is the standard envelope format for all structured responses
// sent from server to client via Server.SendResponse() / BroadcastResponse().
//
// When a Message's Data field is a JSON object starting with {"type":", it is
// interpreted as a ResponseEnvelope. The client dispatches Envelope.Type to the
// corresponding registered TypedResponseHandler.
type ResponseEnvelope struct {
	// Type is the response type classifier (required). Determines which handler
	// is invoked on the client side.
	Type ResponseType `json:"type"`

	// ID optionally associates this response with a prior request, enabling
	// request-response correlation in push-based scenarios.
	ID string `json:"id,omitempty"`

	// SessionID identifies the agent session that produced this response.
	// The TUI uses this field to route events to the correct AgentAnswer.
	SessionID string `json:"session_id,omitempty"`

	// Title is an optional human-readable title shown alongside the response.
	Title string `json:"title,omitempty"`

	// Data carries the type-specific payload. Its structure depends on Type:
	//   - RespTable:  {"headers":[string],"rows":[][]string}
	//   - RespTodo:   []{"id":string,"text":string,"done":bool}
	//   - RespOptions:[]{"value":string,"label":string}
	//   - and so on for each type.
	Data interface{} `json:"data,omitempty"`

	// Meta holds arbitrary extension metadata for advanced use cases.
	Meta map[string]interface{} `json:"meta,omitempty"`
}
