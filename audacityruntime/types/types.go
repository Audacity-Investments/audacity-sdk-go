// Package types contains all public types for the Audacity runtime client,
// matching the Amazon Bedrock Converse API shapes.
package types

// ConversationRole identifies the author of a message.
type ConversationRole string

const (
	ConversationRoleUser      ConversationRole = "user"
	ConversationRoleAssistant ConversationRole = "assistant"
)

// StopReason explains why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn         StopReason = "end_turn"
	StopReasonMaxTokens       StopReason = "max_tokens"
	StopReasonToolUse         StopReason = "tool_use"
	StopReasonStopSequence    StopReason = "stop_sequence"
	StopReasonContentFiltered StopReason = "content_filtered"
)

// Message is a conversation turn.
type Message struct {
	Role    ConversationRole
	Content []ContentBlock
}

// ContentBlock is a sealed union for message content variants.
type ContentBlock interface {
	isContentBlock()
}

// ContentBlockMemberText is a plain-text content block.
type ContentBlockMemberText struct {
	Value string
}

func (*ContentBlockMemberText) isContentBlock() {}

// ImageFormat identifies the media type of an image block.
type ImageFormat string

const (
	ImageFormatPng  ImageFormat = "png"
	ImageFormatJpeg ImageFormat = "jpeg"
	ImageFormatGif  ImageFormat = "gif"
	ImageFormatWebp ImageFormat = "webp"
)

// ImageSource is a sealed union for where an image's data comes from.
type ImageSource interface {
	isImageSource()
}

// ImageSourceMemberBytes carries raw image bytes (Bedrock parity).
type ImageSourceMemberBytes struct {
	Value []byte
}

func (*ImageSourceMemberBytes) isImageSource() {}

// ImageSourceMemberUrl carries an https or data URL (Audacity extension).
type ImageSourceMemberUrl struct {
	Value string
}

func (*ImageSourceMemberUrl) isImageSource() {}

// ImageBlock is the payload of an image content block.
type ImageBlock struct {
	Format ImageFormat
	Source ImageSource
}

// ContentBlockMemberImage is an image content block. Only valid in user
// messages; ignored in assistant messages (Bedrock parity).
type ContentBlockMemberImage struct {
	Value ImageBlock
}

func (*ContentBlockMemberImage) isContentBlock() {}

// ToolUseBlock carries a model-initiated tool invocation.
type ToolUseBlock struct {
	ToolUseId string
	Name      string
	Input     interface{} // any JSON-serialisable value
}

// ContentBlockMemberToolUse is a tool-use content block.
type ContentBlockMemberToolUse struct {
	Value ToolUseBlock
}

func (*ContentBlockMemberToolUse) isContentBlock() {}

// ToolResultContentBlock is a sealed union for tool-result content variants.
type ToolResultContentBlock interface {
	isToolResultContent()
}

// ToolResultContentMemberText is a plain-text tool result entry.
type ToolResultContentMemberText struct {
	Value string
}

func (*ToolResultContentMemberText) isToolResultContent() {}

// ToolResultContentMemberJson is a JSON tool result entry.
type ToolResultContentMemberJson struct {
	Value interface{}
}

func (*ToolResultContentMemberJson) isToolResultContent() {}

// ToolResultBlock is the payload of a tool result block.
type ToolResultBlock struct {
	ToolUseId string
	Content   []ToolResultContentBlock
	Status    *string // "success" | "error" — optional, ignored on the wire
}

// ContentBlockMemberToolResult is a tool-result content block.
type ContentBlockMemberToolResult struct {
	Value ToolResultBlock
}

func (*ContentBlockMemberToolResult) isContentBlock() {}

// SystemContentBlock is a system-prompt content entry.
type SystemContentBlock struct {
	Text string
}

// InferenceConfiguration holds optional inference parameters.
type InferenceConfiguration struct {
	MaxTokens     *int32
	Temperature   *float32
	TopP          *float32
	StopSequences []string
}

// ToolInputSchema wraps a JSON Schema object for a tool's input.
type ToolInputSchema struct {
	Json interface{} // JSON Schema object
}

// ToolSpecification defines a single tool.
type ToolSpecification struct {
	Name        string
	Description *string
	InputSchema *ToolInputSchema
}

// Tool wraps a ToolSpecification.
type Tool struct {
	ToolSpec *ToolSpecification
}

// ToolChoice is a sealed union for how the model selects tools.
type ToolChoice interface {
	isToolChoice()
}

// ToolChoiceMemberAuto lets the model decide whether to call a tool.
type ToolChoiceMemberAuto struct{}

func (*ToolChoiceMemberAuto) isToolChoice() {}

// ToolChoiceMemberAny requires the model to call at least one tool.
type ToolChoiceMemberAny struct{}

func (*ToolChoiceMemberAny) isToolChoice() {}

// SpecificToolChoice names the tool the model must call.
type SpecificToolChoice struct {
	Name string
}

// ToolChoiceMemberTool forces the model to call a specific tool.
type ToolChoiceMemberTool struct {
	Value SpecificToolChoice
}

func (*ToolChoiceMemberTool) isToolChoice() {}

// ToolConfiguration bundles tool definitions and the tool-choice policy.
type ToolConfiguration struct {
	Tools      []Tool
	ToolChoice ToolChoice
}

// ConverseOutput is a sealed union for the Converse operation's output.
type ConverseOutput interface {
	isConverseOutput()
}

// ConverseOutputMemberMessage wraps the assistant's response message.
type ConverseOutputMemberMessage struct {
	Value Message
}

func (*ConverseOutputMemberMessage) isConverseOutput() {}

// TokenUsage reports token consumption for a request.
type TokenUsage struct {
	InputTokens  int32
	OutputTokens int32
	TotalTokens  int32
}

// ConverseMetrics reports latency for a request.
type ConverseMetrics struct {
	LatencyMs int64
}
