package main

type MCPRequestType struct {
	MethodName     string
	ParamType      string
	ResultType     string
	HookName       string
	Group          string
	GroupName      string
	GroupHookName  string
	UnmarshalError string
	HandlerFunc    string
	ResultIsAny    bool // If true, result type is 'any' instead of '*mcp.ResultType'
	HasMeta        bool // If true, request.Params.Meta holds _meta and is extracted into ctx
	// RemovedInModern marks methods removed by protocol version 2026-07-28.
	// They are rejected with METHOD_NOT_FOUND on requests declaring that
	// version or later.
	RemovedInModern bool
	// RequiresModern marks methods introduced by protocol version 2026-07-28.
	// They are rejected with METHOD_NOT_FOUND on legacy requests.
	RequiresModern bool
}

var MCPRequestTypes = []MCPRequestType{
	{
		MethodName:      "MethodInitialize",
		ParamType:       "InitializeRequest",
		ResultType:      "InitializeResult",
		HookName:        "Initialize",
		UnmarshalError:  "invalid initialize request",
		HandlerFunc:     "handleInitialize",
		RemovedInModern: true,
	}, {
		MethodName:      "MethodPing",
		ParamType:       "PingRequest",
		ResultType:      "EmptyResult",
		HookName:        "Ping",
		UnmarshalError:  "invalid ping request",
		HandlerFunc:     "handlePing",
		RemovedInModern: true,
	}, {
		MethodName:     "MethodServerDiscover",
		ParamType:      "DiscoverRequest",
		ResultType:     "DiscoverResult",
		HookName:       "Discover",
		UnmarshalError: "invalid discover request",
		HandlerFunc:    "handleDiscover",
		RequiresModern: true,
	}, {
		MethodName:     "MethodSubscriptionsListen",
		ParamType:      "SubscriptionsListenRequest",
		ResultType:     "SubscriptionsListenResult",
		HookName:       "SubscriptionsListen",
		UnmarshalError: "invalid subscriptions listen request",
		HandlerFunc:    "handleSubscriptionsListen",
		RequiresModern: true,
	}, {
		MethodName:      "MethodSetLogLevel",
		ParamType:       "SetLevelRequest",
		ResultType:      "EmptyResult",
		Group:           "logging",
		GroupName:       "Logging",
		GroupHookName:   "Logging",
		HookName:        "SetLevel",
		UnmarshalError:  "invalid set level request",
		HandlerFunc:     "handleSetLevel",
		RemovedInModern: true,
	}, {
		MethodName:     "MethodResourcesList",
		ParamType:      "ListResourcesRequest",
		ResultType:     "ListResourcesResult",
		Group:          "resources",
		GroupName:      "Resources",
		GroupHookName:  "Resource",
		HookName:       "ListResources",
		UnmarshalError: "invalid list resources request",
		HandlerFunc:    "handleListResources",
		HasMeta:        true,
	}, {
		MethodName:     "MethodResourcesTemplatesList",
		ParamType:      "ListResourceTemplatesRequest",
		ResultType:     "ListResourceTemplatesResult",
		Group:          "resources",
		GroupName:      "Resources",
		GroupHookName:  "Resource",
		HookName:       "ListResourceTemplates",
		UnmarshalError: "invalid list resource templates request",
		HandlerFunc:    "handleListResourceTemplates",
		HasMeta:        true,
	}, {
		MethodName:     "MethodResourcesRead",
		ParamType:      "ReadResourceRequest",
		ResultType:     "ReadResourceResult",
		Group:          "resources",
		GroupName:      "Resources",
		GroupHookName:  "Resource",
		HookName:       "ReadResource",
		UnmarshalError: "invalid read resource request",
		HandlerFunc:    "handleReadResource",
		HasMeta:        true,
	}, {
		MethodName:      "MethodResourcesSubscribe",
		ParamType:       "SubscribeRequest",
		ResultType:      "EmptyResult",
		Group:           "resources",
		GroupName:       "Resources",
		GroupHookName:   "Resource",
		HookName:        "Subscribe",
		UnmarshalError:  "invalid subscribe request",
		HandlerFunc:     "handleSubscribe",
		RemovedInModern: true,
	}, {
		MethodName:      "MethodResourcesUnsubscribe",
		ParamType:       "UnsubscribeRequest",
		ResultType:      "EmptyResult",
		Group:           "resources",
		GroupName:       "Resources",
		GroupHookName:   "Resource",
		HookName:        "Unsubscribe",
		UnmarshalError:  "invalid unsubscribe request",
		HandlerFunc:     "handleUnsubscribe",
		RemovedInModern: true,
	}, {
		MethodName:     "MethodPromptsList",
		ParamType:      "ListPromptsRequest",
		ResultType:     "ListPromptsResult",
		Group:          "prompts",
		GroupName:      "Prompts",
		GroupHookName:  "Prompt",
		HookName:       "ListPrompts",
		UnmarshalError: "invalid list prompts request",
		HandlerFunc:    "handleListPrompts",
		HasMeta:        true,
	}, {
		MethodName:     "MethodPromptsGet",
		ParamType:      "GetPromptRequest",
		ResultType:     "GetPromptResult",
		Group:          "prompts",
		GroupName:      "Prompts",
		GroupHookName:  "Prompt",
		HookName:       "GetPrompt",
		UnmarshalError: "invalid get prompt request",
		HandlerFunc:    "handleGetPrompt",
		HasMeta:        true,
	}, {
		MethodName:     "MethodToolsList",
		ParamType:      "ListToolsRequest",
		ResultType:     "ListToolsResult",
		Group:          "tools",
		GroupName:      "Tools",
		GroupHookName:  "Tool",
		HookName:       "ListTools",
		UnmarshalError: "invalid list tools request",
		HandlerFunc:    "handleListTools",
		HasMeta:        true,
	}, {
		MethodName:     "MethodToolsCall",
		ParamType:      "CallToolRequest",
		ResultType:     "CallToolResult",
		Group:          "tools",
		GroupName:      "Tools",
		GroupHookName:  "Tool",
		HookName:       "CallTool",
		UnmarshalError: "invalid call tool request",
		HandlerFunc:    "handleToolCall",
		ResultIsAny:    true, // Returns 'any' to support both CallToolResult and CreateTaskResult
		HasMeta:        true,
	}, {
		MethodName:     "MethodTasksGet",
		ParamType:      "GetTaskRequest",
		ResultType:     "GetTaskResult",
		Group:          "tasks",
		GroupName:      "Tasks",
		GroupHookName:  "Task",
		HookName:       "GetTask",
		UnmarshalError: "invalid get task request",
		HandlerFunc:    "handleGetTask",
	}, {
		MethodName:      "MethodTasksList",
		ParamType:       "ListTasksRequest",
		ResultType:      "ListTasksResult",
		Group:           "tasks",
		GroupName:       "Tasks",
		GroupHookName:   "Task",
		HookName:        "ListTasks",
		UnmarshalError:  "invalid list tasks request",
		HandlerFunc:     "handleListTasks",
		RemovedInModern: true,
	}, {
		MethodName:      "MethodTasksResult",
		ParamType:       "TaskResultRequest",
		ResultType:      "TaskResultResult",
		Group:           "tasks",
		GroupName:       "Tasks",
		GroupHookName:   "Task",
		HookName:        "TaskResult",
		UnmarshalError:  "invalid task result request",
		HandlerFunc:     "handleTaskResult",
		RemovedInModern: true,
	}, {
		MethodName:     "MethodTasksCancel",
		ParamType:      "CancelTaskRequest",
		ResultType:     "CancelTaskResult",
		Group:          "tasks",
		GroupName:      "Tasks",
		GroupHookName:  "Task",
		HookName:       "CancelTask",
		UnmarshalError: "invalid cancel task request",
		HandlerFunc:    "handleCancelTask",
	}, {
		MethodName:     "MethodCompletionComplete",
		ParamType:      "CompleteRequest",
		ResultType:     "CompleteResult",
		Group:          "completions",
		GroupName:      "Completions",
		GroupHookName:  "Completion",
		HookName:       "Complete",
		UnmarshalError: "invalid completion request",
		HandlerFunc:    "handleComplete",
	},
}
