package agent

import "testing"

func TestIsContextWindowError(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		// ── True positives: these ARE context window errors ──────────
		{
			name:   "OpenAI context length exceeded",
			errMsg: "This model's maximum context length is 128000 tokens. Your messages resulted in 130000 tokens.",
			want:   true,
		},
		{
			name:   "OpenAI reduce length",
			errMsg: "Please reduce the length of the messages.",
			want:   true,
		},
		{
			name:   "Anthropic content_length_exceeded",
			errMsg: "error: content_length_exceeded: Output blocked by content_length_exceeded",
			want:   true,
		},
		{
			name:   "Generic context_length_exceeded",
			errMsg: "context_length_exceeded",
			want:   true,
		},
		{
			name:   "Generic max_tokens",
			errMsg: "max_tokens exceeded for this request",
			want:   true,
		},
		{
			name:   "Token limit compound",
			errMsg: "The request exceeded the token limit for this model",
			want:   true,
		},
		{
			name:   "Context window reference",
			errMsg: "Input exceeds the context window for this model",
			want:   true,
		},
		{
			name:   "Too many tokens compound",
			errMsg: "Too many tokens in the request: 200000",
			want:   true,
		},
		{
			name:   "HTTP 413 payload too large",
			errMsg: "413 Payload Too Large",
			want:   true,
		},
		{
			name:   "Prompt too long",
			errMsg: "prompt is too long (150000 tokens)",
			want:   true,
		},
		{
			name:   "Vertex AI string too long",
			errMsg: "string too long: input exceeds model capacity",
			want:   true,
		},
		{
			name:   "Total tokens exceeded",
			errMsg: "total tokens exceeded: 200000 > 128000",
			want:   true,
		},
		{
			name:   "Input too long",
			errMsg: "input too long",
			want:   true,
		},
		{
			name:   "Model maximum context",
			errMsg: "This exceeds the model's maximum context of 128k tokens",
			want:   true,
		},
		{
			name:   "Reduce your prompt",
			errMsg: "Please reduce your prompt to fit within the model's context",
			want:   true,
		},
		{
			name:   "Maximum model length",
			errMsg: "maximum model length is 200000 tokens",
			want:   true,
		},

		// ── True negatives: these are NOT context window errors ──────
		{
			name:   "Auth error with token word",
			errMsg: "invalid token: the API token provided is not valid",
			want:   false,
		},
		{
			name:   "Token expired",
			errMsg: "token expired: please refresh your authentication token",
			want:   false,
		},
		{
			name:   "Token revoked",
			errMsg: "token revoked by administrator",
			want:   false,
		},
		{
			name:   "401 unauthorized with token",
			errMsg: "401 Unauthorized: invalid bearer token",
			want:   false,
		},
		{
			name:   "403 forbidden",
			errMsg: "403 Forbidden: access denied for this token",
			want:   false,
		},
		{
			name:   "Rate limit with token word",
			errMsg: "429 Too Many Requests: token rate limit exceeded",
			want:   false,
		},
		{
			name:   "Quota exceeded",
			errMsg: "quota exceeded for this API key token",
			want:   false,
		},
		{
			name:   "Invalid API key",
			errMsg: "invalid api key provided",
			want:   false,
		},
		{
			name:   "Generic connection error",
			errMsg: "connection refused: could not reach the server",
			want:   false,
		},
		{
			name:   "Timeout error",
			errMsg: "request timeout after 30 seconds",
			want:   false,
		},
		{
			name:   "JSON parse error with length",
			errMsg: "json: cannot unmarshal string into Go struct field .length",
			want:   false,
		},
		{
			name:   "Empty error",
			errMsg: "",
			want:   false,
		},
		{
			name:   "Random error",
			errMsg: "something went wrong internally",
			want:   false,
		},
		{
			name:   "Authentication failure",
			errMsg: "authentication failed: please check your credentials",
			want:   false,
		},
		{
			name:   "503 service unavailable",
			errMsg: "503 Service Unavailable",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isContextWindowError(tt.errMsg)
			if got != tt.want {
				t.Errorf("isContextWindowError(%q) = %v, want %v", tt.errMsg, got, tt.want)
			}
		})
	}
}
