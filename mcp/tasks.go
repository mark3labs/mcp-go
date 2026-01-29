package mcp

import (
	"time"
)

// TaskOption is a function that configures a Task.
// It provides a flexible way to set various properties of a Task using the functional options pattern.
type TaskOption func(*Task)

//
// Core Task Functions
//

// NewTask creates a new Task with the given ID and options.
// The task will be configured based on the provided options.
// Options are applied in order, allowing for flexible task configuration.
func NewTask(taskId string, opts ...TaskOption) Task {
	now := time.Now().UTC().Format(time.RFC3339)
	task := Task{
		TaskId:        taskId,
		Status:        TaskStatusWorking,
		CreatedAt:     now,
		LastUpdatedAt: now,
	}

	for _, opt := range opts {
		opt(&task)
	}

	return task
}

// WithTaskStatus sets the status of the task.
func WithTaskStatus(status TaskStatus) TaskOption {
	return func(t *Task) {
		t.Status = status
	}
}

// WithTaskStatusMessage sets a human-readable status message for the task.
func WithTaskStatusMessage(message string) TaskOption {
	return func(t *Task) {
		t.StatusMessage = message
	}
}

// WithTaskTTL sets the time-to-live for the task in milliseconds.
// After this duration from creation, the task may be deleted.
func WithTaskTTL(ttlMs int64) TaskOption {
	return func(t *Task) {
		t.TTL = &ttlMs
	}
}

// WithTaskPollInterval sets the suggested polling interval in milliseconds.
func WithTaskPollInterval(intervalMs int64) TaskOption {
	return func(t *Task) {
		t.PollInterval = &intervalMs
	}
}

// WithTaskCreatedAt sets a specific creation timestamp for the task.
// By default, NewTask uses the current time.
func WithTaskCreatedAt(createdAt string) TaskOption {
	return func(t *Task) {
		t.CreatedAt = createdAt
	}
}

// WithTaskLastUpdatedAt sets a specific last updated timestamp for the task.
// By default, NewTask uses the current time.
func WithTaskLastUpdatedAt(lastUpdatedAt string) TaskOption {
	return func(t *Task) {
		t.LastUpdatedAt = lastUpdatedAt
	}
}

//
// Task Helper Functions
//

// NewTaskParams creates TaskParams with the given TTL.
func NewTaskParams(ttlMs *int64) TaskParams {
	return TaskParams{
		TTL: ttlMs,
	}
}

// NewCreateTaskResult creates a CreateTaskResult with the given task.
func NewCreateTaskResult(task Task) CreateTaskResult {
	return CreateTaskResult{
		Task: task,
	}
}

// NewGetTaskResult creates a GetTaskResult from a Task.
func NewGetTaskResult(task Task) GetTaskResult {
	return GetTaskResult{
		Task: task,
	}
}

// NewListTasksResult creates a ListTasksResult with the given tasks.
func NewListTasksResult(tasks []Task) ListTasksResult {
	return ListTasksResult{
		Tasks: tasks,
	}
}

// NewCancelTaskResult creates a CancelTaskResult from a Task.
func NewCancelTaskResult(task Task) CancelTaskResult {
	return CancelTaskResult{
		Task: task,
	}
}

// NewTaskStatusNotification creates a notification for a task status change.
func NewTaskStatusNotification(task Task) TaskStatusNotification {
	return TaskStatusNotification{
		Notification: Notification{
			Method: string(MethodNotificationTasksStatus),
		},
		Params: TaskStatusNotificationParams{
			Task: task,
		},
	}
}

//
// Related Task Metadata
//

// RelatedTaskMetaKey is the standard metadata key for associating a message with a task.
// This follows the MCP specification for task-related metadata.
const RelatedTaskMetaKey = "io.modelcontextprotocol/related-task"

// RelatedTaskMeta creates the metadata map for associating a message with a task.
// This is used to link responses, notifications, or results to their originating task.
func RelatedTaskMeta(taskID string) map[string]any {
	return map[string]any{
		"taskId": taskID,
	}
}

// WithRelatedTask creates a Meta object with the related task ID set.
// This is a convenience function for adding task association metadata to responses.
//
// Example:
//
//	result := &mcp.CallToolResult{
//	    Result: mcp.Result{
//	        Meta: mcp.WithRelatedTask(taskID),
//	    },
//	}
func WithRelatedTask(taskID string) *Meta {
	return &Meta{
		AdditionalFields: map[string]any{
			RelatedTaskMetaKey: RelatedTaskMeta(taskID),
		},
	}
}

//
// Task Capability Helper Functions
//

// NewTasksCapability creates a TasksCapability with all operations enabled.
func NewTasksCapability() *TasksCapability {
	return &TasksCapability{
		List:   &struct{}{},
		Cancel: &struct{}{},
		Requests: &TaskRequestsCapability{
			Tools: &struct {
				Call *struct{} `json:"call,omitempty"`
			}{
				Call: &struct{}{},
			},
		},
	}
}

// NewTasksCapabilityWithToolsOnly creates a TasksCapability with only tool call support.
// List and Cancel operations are not enabled with this capability.
func NewTasksCapabilityWithToolsOnly() *TasksCapability {
	return &TasksCapability{
		Requests: &TaskRequestsCapability{
			Tools: &struct {
				Call *struct{} `json:"call,omitempty"`
			}{
				Call: &struct{}{},
			},
		},
	}
}
