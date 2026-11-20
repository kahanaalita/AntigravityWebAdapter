package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	fhttp "github.com/bogdanfinn/fhttp"
	"golang.org/x/sync/errgroup"

	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/geminiweb"
)

const (
	scottyStartURL      = "https://content-push.googleapis.com/upload/"
	defaultScottyPushID = "feeds/mcudyrk2a4khkz"
	scottyPushID        = defaultScottyPushID
)

// GetScottyPushID returns the push ID configured via SCOTTY_PUSH_ID or default.
func GetScottyPushID() string {
	if val := strings.TrimSpace(os.Getenv("SCOTTY_PUSH_ID")); val != "" {
		return val
	}
	return defaultScottyPushID
}

// ScottyUploader handles uploading media files to Google AI Pro storage.
type ScottyUploader struct {
	session *auth.SessionManager
}

// NewScottyUploader creates a ScottyUploader.
func NewScottyUploader(session *auth.SessionManager) *ScottyUploader {
	return &ScottyUploader{session: session}
}

// UploadImages uploads multiple images in parallel using errgroup and returns Scotty attachments.
func (u *ScottyUploader) UploadImages(ctx context.Context, images []ResolvedImage) ([]geminiweb.ScottyAttachment, error) {
	if len(images) == 0 {
		return nil, nil
	}

	attachments := make([]geminiweb.ScottyAttachment, len(images))
	g, gCtx := errgroup.WithContext(ctx)

	for i, img := range images {
		idx := i
		targetImg := img

		g.Go(func() error {
			att, err := u.UploadSingle(gCtx, targetImg)
			if err != nil {
				return fmt.Errorf("upload image %s (%d): %w", targetImg.FileName, idx, err)
			}
			attachments[idx] = *att
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return attachments, nil
}

// UploadSingle performs the two-phase Scotty Resumable Upload for a single image.
func (u *ScottyUploader) UploadSingle(ctx context.Context, img ResolvedImage) (*geminiweb.ScottyAttachment, error) {
	if len(img.Data) == 0 {
		return nil, errors.New("cannot upload empty image")
	}

	cookieHdr := u.session.Store().CookieHeader()
	client := u.session.TLSClient()

	// Phase 1: START
	reqStart, err := fhttp.NewRequestWithContext(ctx, "POST", scottyStartURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create scotty start req: %w", err)
	}

	reqStart.Header.Set("X-Goog-Upload-Command", "start")
	reqStart.Header.Set("X-Goog-Upload-Protocol", "resumable")
	reqStart.Header.Set("X-Goog-Upload-Header-Content-Length", strconv.Itoa(len(img.Data)))
	reqStart.Header.Set("X-Goog-Upload-Header-Content-Type", img.MimeType)
	reqStart.Header.Set("Push-Id", GetScottyPushID())
	reqStart.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")
	if cookieHdr != "" {
		reqStart.Header.Set("Cookie", cookieHdr)
	}

	respStart, err := client.Do(reqStart)
	if err != nil {
		return nil, fmt.Errorf("execute scotty start: %w", err)
	}
	defer respStart.Body.Close()

	if respStart.StatusCode != 200 {
		snippet, _ := io.ReadAll(io.LimitReader(respStart.Body, 512))
		return nil, fmt.Errorf("scotty start status %d: %s", respStart.StatusCode, string(snippet))
	}

	uploadURL := respStart.Header.Get("X-Goog-Upload-URL")
	if uploadURL == "" {
		for k, v := range respStart.Header {
			if strings.EqualFold(k, "X-Goog-Upload-URL") && len(v) > 0 && v[0] != "" {
				uploadURL = v[0]
				break
			}
		}
	}
	if uploadURL == "" {
		return nil, errors.New("scotty start did not return X-Goog-Upload-URL header")
	}

	// Phase 2: UPLOAD, FINALIZE
	reqUpload, err := fhttp.NewRequestWithContext(ctx, "POST", uploadURL, bytes.NewReader(img.Data))
	if err != nil {
		return nil, fmt.Errorf("create scotty upload req: %w", err)
	}

	reqUpload.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	reqUpload.Header.Set("X-Goog-Upload-Offset", "0")
	reqUpload.Header.Set("Content-Type", img.MimeType)
	reqUpload.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")
	if cookieHdr != "" {
		reqUpload.Header.Set("Cookie", cookieHdr)
	}

	respUpload, err := client.Do(reqUpload)
	if err != nil {
		return nil, fmt.Errorf("execute scotty upload finalize: %w", err)
	}
	defer respUpload.Body.Close()

	uploadRespBytes, err := io.ReadAll(respUpload.Body)
	if err != nil {
		return nil, fmt.Errorf("read scotty upload response: %w", err)
	}

	if respUpload.StatusCode != 200 {
		return nil, fmt.Errorf("scotty upload status %d: %s", respUpload.StatusCode, string(uploadRespBytes))
	}

	uploadRef := extractUploadRef(string(uploadRespBytes), respUpload.Header)
	if uploadRef == "" {
		snippet := strings.TrimSpace(string(uploadRespBytes))
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		if snippet == "" {
			snippet = "<empty>"
		}
		return nil, fmt.Errorf("scotty upload failed to extract uploadRef (status %d, snippet %s)", respUpload.StatusCode, snippet)
	}

	return &geminiweb.ScottyAttachment{
		UploadRef: uploadRef,
		FileName:  img.FileName,
		MimeType:  img.MimeType,
	}, nil
}

func extractUploadRef(body string, hdr fhttp.Header) string {
	// 1. Try header (case-insensitive for HTTP/2)
	for k, v := range hdr {
		if strings.EqualFold(k, "X-Goog-Upload-Status-Resource-Id") && len(v) > 0 && v[0] != "" {
			return v[0]
		}
	}
	if ref := hdr.Get("X-Goog-Upload-Status-Resource-Id"); ref != "" {
		return ref
	}

	// 2. Structured JSON
	var rupioResp map[string]any
	if err := json.Unmarshal([]byte(body), &rupioResp); err == nil {
		if token := findAttachmentToken(rupioResp); token != "" {
			return token
		}
	}

	// 3. String contains /contrib_service/
	if idx := strings.Index(body, "/contrib_service/"); idx >= 0 {
		sub := body[idx:]
		end := strings.IndexAny(sub, "\"' \r\n\t,")
		if end > 0 {
			return sub[:end]
		}
		return strings.TrimSpace(sub)
	}

	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}

	return ""
}

func findAttachmentToken(node any) string {
	switch v := node.(type) {
	case map[string]any:
		// Priority 1: Direct known keys in current map
		for _, key := range []string{"photopicker_service_attachment_token", "attachment_token", "upload_ref", "resourceId"} {
			if val, ok := v[key].(string); ok && val != "" {
				return val
			}
		}
		// Priority 2: resource.url or resource.id
		if res, ok := v["resource"].(map[string]any); ok {
			if u, ok := res["url"].(string); ok && u != "" {
				return u
			}
			if id, ok := res["id"].(string); ok && id != "" {
				return id
			}
		}
		// Priority 3: any key substring matching
		for k, val := range v {
			if strings.Contains(k, "attachment_token") || k == "resourceId" || k == "upload_ref" {
				if s, ok := val.(string); ok && s != "" {
					return s
				}
			}
		}
		// Priority 4: recurse into children
		for _, val := range v {
			if token := findAttachmentToken(val); token != "" {
				return token
			}
		}
	case []any:
		for _, item := range v {
			if token := findAttachmentToken(item); token != "" {
				return token
			}
		}
	}
	return ""
}
