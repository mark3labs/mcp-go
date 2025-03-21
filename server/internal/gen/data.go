package main

type MCPRequestType struct {
	MethodName        string
	ParamType         string
	ResultType        string
	CallbackName      string
	Group             string
	GroupName         string
	GroupCallbackName string
	UnmarshalError    string
	HandlerFunc       string
}

var MCPRequestTypes = []MCPRequestType{
	{
		MethodName:     "MethodInitialize",
		ParamType:      "InitializeRequest",
		ResultType:     "InitializeResult",
		CallbackName:   "Initialize",
		UnmarshalError: "Invalid initialize request",
		HandlerFunc:    "handleInitialize",
	}, {
		MethodName:     "MethodPing",
		ParamType:      "PingRequest",
		ResultType:     "EmptyResult",
		CallbackName:   "Ping",
		UnmarshalError: "Invalid ping request",
		HandlerFunc:    "handlePing",
	}, {
		MethodName:        "MethodResourcesList",
		ParamType:         "ListResourcesRequest",
		ResultType:        "ListResourcesResult",
		Group:             "resources",
		GroupName:         "Resources",
		GroupCallbackName: "Resource",
		CallbackName:      "ListResources",
		UnmarshalError:    "Invalid list resources request",
		HandlerFunc:       "handleListResources",
	}, {
		MethodName:        "MethodResourcesTemplatesList",
		ParamType:         "ListResourceTemplatesRequest",
		ResultType:        "ListResourceTemplatesResult",
		Group:             "resources",
		GroupName:         "Resources",
		GroupCallbackName: "Resource",
		CallbackName:      "ListResourceTemplates",
		UnmarshalError:    "Invalid list resource templates request",
		HandlerFunc:       "handleListResourceTemplates",
	}, {
		MethodName:        "MethodResourcesRead",
		ParamType:         "ReadResourceRequest",
		ResultType:        "ReadResourceResult",
		Group:             "resources",
		GroupName:         "Resources",
		GroupCallbackName: "Resource",
		CallbackName:      "ReadResource",
		UnmarshalError:    "Invalid read resource request",
		HandlerFunc:       "handleReadResource",
	}, {
		MethodName:        "MethodPromptsList",
		ParamType:         "ListPromptsRequest",
		ResultType:        "ListPromptsResult",
		Group:             "prompts",
		GroupName:         "Prompts",
		GroupCallbackName: "Prompt",
		CallbackName:      "ListPrompts",
		UnmarshalError:    "Invalid list prompts request",
		HandlerFunc:       "handleListPrompts",
	}, {
		MethodName:        "MethodPromptsGet",
		ParamType:         "GetPromptRequest",
		ResultType:        "GetPromptResult",
		Group:             "prompts",
		GroupName:         "Prompts",
		GroupCallbackName: "Prompt",
		CallbackName:      "GetPrompt",
		UnmarshalError:    "Invalid get prompt request",
		HandlerFunc:       "handleGetPrompt",
	}, {
		MethodName:        "MethodToolsList",
		ParamType:         "ListToolsRequest",
		ResultType:        "ListToolsResult",
		Group:             "tools",
		GroupName:         "Tools",
		GroupCallbackName: "Tool",
		CallbackName:      "ListTools",
		UnmarshalError:    "Invalid list tools request",
		HandlerFunc:       "handleListTools",
	}, {
		MethodName:        "MethodToolsCall",
		ParamType:         "CallToolRequest",
		ResultType:        "CallToolResult",
		Group:             "tools",
		GroupName:         "Tools",
		GroupCallbackName: "Tool",
		CallbackName:      "CallTool",
		UnmarshalError:    "Invalid call tool request",
		HandlerFunc:       "handleToolCall",
	},
}
