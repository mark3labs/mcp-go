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
}

var MCPRequestTypes = []MCPRequestType{
	{
		MethodName:     "MethodInitialize",
		ParamType:      "InitializeRequest",
		ResultType:     "InitializeResult",
		HookName:       "Initialize",
		UnmarshalError: "invalid initialize request",
		HandlerFunc:    "handleInitialize",
	}, {
		MethodName:     "MethodPing",
		ParamType:      "PingRequest",
		ResultType:     "EmptyResult",
		HookName:       "Ping",
		UnmarshalError: "invalid ping request",
		HandlerFunc:    "handlePing",
	}, {
		MethodName:     "MethodSetLogLevel",
		ParamType:      "SetLevelRequest",
		ResultType:     "EmptyResult",
		Group:          "logging",
		GroupName:      "Logging",
		GroupHookName:  "Logging",
		HookName:       "SetLevel",
		UnmarshalError: "invalid set level request",
		HandlerFunc:    "handleSetLevel",
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
	},
}
