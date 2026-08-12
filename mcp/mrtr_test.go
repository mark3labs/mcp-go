package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInputRequestRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		request InputRequest
		wire    string
	}{
		{
			name: "elicitation",
			request: NewElicitationInputRequest(ElicitationParams{
				Mode:    ElicitationModeForm,
				Message: "What is your name?",
			}),
			wire: `{"method":"elicitation/create","params":{"mode":"form","message":"What is your name?"}}`,
		},
		{
			name: "sampling",
			request: NewSamplingInputRequest(CreateMessageParams{
				Messages:  []SamplingMessage{{Role: RoleUser, Content: NewTextContent("hi")}},
				MaxTokens: 100,
			}),
			wire: `{"method":"sampling/createMessage","params":{"messages":[{"role":"user","content":{"type":"text","text":"hi"}}],"maxTokens":100}}`,
		},
		{
			name:    "roots",
			request: NewRootsInputRequest(),
			wire:    `{"method":"roots/list"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.request)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wire, string(encoded))

			var decoded InputRequest
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			assert.Equal(t, tt.request.Method, decoded.Method)

			switch tt.request.Method {
			case MethodElicitationCreate:
				require.NotNil(t, decoded.Elicitation)
				assert.Equal(t, tt.request.Elicitation.Message, decoded.Elicitation.Message)
			case MethodSamplingCreateMessage:
				require.NotNil(t, decoded.Sampling)
				assert.Equal(t, tt.request.Sampling.MaxTokens, decoded.Sampling.MaxTokens)
			case MethodListRoots:
				require.NotNil(t, decoded.Roots)
			}
		})
	}
}

func TestInputRequestRejectsUnknownMethods(t *testing.T) {
	var request InputRequest
	err := json.Unmarshal([]byte(`{"method":"tools/call","params":{}}`), &request)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported input request method")

	_, err = json.Marshal(InputRequest{Method: "tools/call"})
	require.Error(t, err)
}

func TestInputResponseDecodesAgainstItsRequest(t *testing.T) {
	// An input response carries no method of its own: the variant is decided
	// by the request it answers, so decoding is deferred until they are paired
	// up.
	requests := InputRequests{
		"who":   NewElicitationInputRequest(ElicitationParams{Message: "name?"}),
		"guess": NewSamplingInputRequest(CreateMessageParams{MaxTokens: 10}),
		"where": NewRootsInputRequest(),
	}

	wire := `{
		"who":   {"action":"accept","content":{"name":"Ada"}},
		"guess": {"model":"test-model","role":"assistant","content":{"type":"text","text":"hi"}},
		"where": {"roots":[{"uri":"file:///tmp","name":"tmp"}]}
	}`

	var responses InputResponses
	require.NoError(t, json.Unmarshal([]byte(wire), &responses))
	require.NoError(t, responses.DecodeFor(requests))

	require.NotNil(t, responses["who"].Elicitation)
	assert.Equal(t, ElicitationResponseActionAccept, responses["who"].Elicitation.Action)

	require.NotNil(t, responses["guess"].Sampling)
	assert.Equal(t, "test-model", responses["guess"].Sampling.Model)

	require.NotNil(t, responses["where"].Roots)
	require.Len(t, responses["where"].Roots.Roots, 1)
	assert.Equal(t, "file:///tmp", responses["where"].Roots.Roots[0].URI)
}

func TestInputResponseRoundTrip(t *testing.T) {
	response := NewElicitationInputResponse(ElicitationResult{
		ElicitationResponse: ElicitationResponse{
			Action:  ElicitationResponseActionAccept,
			Content: map[string]any{"name": "Ada"},
		},
	})

	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"accept","content":{"name":"Ada"}}`, string(encoded))

	var decoded InputResponse
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.NoError(t, decoded.DecodeFor(MethodElicitationCreate))
	assert.Equal(t, ElicitationResponseActionAccept, decoded.Elicitation.Action)
}

func TestInputResponsesDecodeForIgnoresUnpairedKeys(t *testing.T) {
	requests := InputRequests{"known": NewRootsInputRequest()}

	var responses InputResponses
	require.NoError(t, json.Unmarshal([]byte(`{"known":{"roots":[]},"stray":{}}`), &responses))

	// A response the server did not ask for is left alone rather than
	// failing the whole exchange.
	require.NoError(t, responses.DecodeFor(requests))
	assert.NotNil(t, responses["known"].Roots)
	assert.Nil(t, responses["stray"].Roots)
}

func TestNewInputRequiredResult(t *testing.T) {
	result := NewInputRequiredResult(
		InputRequests{"who": NewRootsInputRequest()},
		"opaque-state",
	)

	assert.True(t, result.NeedsInput())
	assert.Equal(t, ResultTypeInputRequired, result.ResultType)
	assert.Equal(t, "opaque-state", result.RequestState)

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"resultType": "input_required",
		"requestState": "opaque-state",
		"inputRequests": {"who": {"method": "roots/list"}}
	}`, string(encoded))
}

func TestCallToolResultCarriesMultiRoundTripFields(t *testing.T) {
	result := &CallToolResult{
		Result: Result{ResultType: ResultTypeInputRequired},
		MultiRoundTripResult: MultiRoundTripResult{
			InputRequests: InputRequests{"who": NewRootsInputRequest()},
			RequestState:  "step=1",
		},
	}
	assert.True(t, result.NeedsInput())

	encoded, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded CallToolResult
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.True(t, decoded.NeedsInput())
	assert.Equal(t, "step=1", decoded.RequestState)
	require.Contains(t, decoded.InputRequests, "who")
	assert.Equal(t, MethodListRoots, decoded.InputRequests["who"].Method)
}

func TestCallToolResultOmitsMultiRoundTripFieldsWhenUnused(t *testing.T) {
	// An ordinary result answered to a client on an earlier protocol version
	// must look exactly as it did before this revision.
	result := NewToolResultText("hello")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":[{"type":"text","text":"hello"}]}`, string(encoded))
}

func TestCallToolParamsCarryInputResponses(t *testing.T) {
	params := CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"formal": true},
		MultiRoundTripParams: MultiRoundTripParams{
			RequestState: "step=1",
			InputResponses: InputResponses{
				"who": NewElicitationInputResponse(ElicitationResult{
					ElicitationResponse: ElicitationResponse{Action: ElicitationResponseActionAccept},
				}),
			},
		},
	}

	encoded, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded CallToolParams
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, "greet", decoded.Name)
	assert.Equal(t, "step=1", decoded.RequestState)
	require.Contains(t, decoded.InputResponses, "who")

	response := decoded.InputResponses["who"]
	require.NoError(t, response.DecodeFor(MethodElicitationCreate))
	assert.Equal(t, ElicitationResponseActionAccept, response.Elicitation.Action)
}

func TestCallToolParamsPreserveRawArgumentsAlongsideRoundTripFields(t *testing.T) {
	// Raw arguments are preserved to avoid precision loss; that must not drop
	// the multi round-trip fields travelling beside them.
	wire := `{
		"name": "inc",
		"arguments": {"n": 9007199254740993},
		"requestState": "step=2",
		"inputResponses": {"who": {"roots": []}}
	}`

	var params CallToolParams
	require.NoError(t, json.Unmarshal([]byte(wire), &params))
	assert.Equal(t, "step=2", params.RequestState)
	require.Contains(t, params.InputResponses, "who")

	encoded, err := json.Marshal(params)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "9007199254740993", "raw arguments survive")
	assert.Contains(t, string(encoded), "step=2")
}
