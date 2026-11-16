package geminiweb

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"agy-gemini-web-mcp/internal/config"
)

// ScottyAttachment represents an uploaded image reference in Google AI Pro.
type ScottyAttachment struct {
	UploadRef string `json:"upload_ref"` // e.g. "/contrib_service/ttl_1d/..."
	FileName  string `json:"file_name"`  // e.g. "image.png"
	MimeType  string `json:"mime_type"`  // e.g. "image/png"
}

// ConversationContext holds context IDs to continue existing chats.
type ConversationContext struct {
	ConversationID string
	ResponseID     string
	ChoiceID       string
}

// BuildExtensionHeader creates the x-goog-ext-525001261-jspb header value.
// Format: [1, null, null, null, "<MODEL_HEX_ID>", null, null, <EFFORT_INT>]
func BuildExtensionHeader(modelHexID string, tier config.EffortTier) string {
	ext := []any{
		1,
		nil,
		nil,
		nil,
		modelHexID,
		nil,
		nil,
		int(tier),
	}
	bytes, _ := json.Marshal(ext)
	return string(bytes)
}

// BuildStreamGenerateBody constructs the URL-encoded body: f.req=<json>&at=<csrf_token>.
func BuildStreamGenerateBody(prompt string, attachments []ScottyAttachment, conv ConversationContext, tier config.EffortTier, atToken string) (string, error) {
	// 1. Build attachment slots
	var attachmentList []any
	for _, att := range attachments {
		mime := att.MimeType
		if mime == "" {
			mime = "image/png"
		}
		name := att.FileName
		if name == "" {
			name = "image.png"
		}

		attachmentItem := []any{
			[]any{att.UploadRef, 1, nil, mime},
			name,
		}
		attachmentList = append(attachmentList, attachmentItem)
	}

	// 2. Build prompt content array
	// [prompt, 0, null, attachments]
	var promptContent []any
	if len(attachmentList) > 0 {
		promptContent = []any{prompt, 0, nil, attachmentList}
	} else {
		promptContent = []any{prompt, 0, nil, nil}
	}

	// 3. Conversation tuple
	// ["<CONVERSATION_ID>", "<RESPONSE_ID>", "<CHOICE_ID>", null, null, []]
	convTuple := []any{
		conv.ConversationID,
		conv.ResponseID,
		conv.ChoiceID,
		nil,
		nil,
		[]any{},
	}

	// 4. Reasoning effort config slot
	effortSlot := []any{nil, int(tier)}

	locale := os.Getenv("GEMINI_LOCALE")
	if locale == "" {
		locale = "ru"
	}

	// 5. Root f.req inner structure
	// [ promptContent, [locale], convTuple, null, null, null, effortSlot ]
	reqPayload := []any{
		promptContent,
		[]string{locale},
		convTuple,
		nil,
		nil,
		nil,
		effortSlot,
	}

	innerBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("marshal inner payload: %w", err)
	}

	outerPayload := []any{nil, string(innerBytes)}
	outerBytes, err := json.Marshal(outerPayload)
	if err != nil {
		return "", fmt.Errorf("marshal outer payload: %w", err)
	}

	form := url.Values{}
	form.Set("f.req", string(outerBytes))
	form.Set("at", atToken)

	return form.Encode(), nil
}
