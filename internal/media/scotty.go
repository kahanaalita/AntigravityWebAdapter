package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	fhttp "github.com/bogdanfinn/fhttp"
	"golang.org/x/sync/errgroup"

	"agy-gemini-web-mcp/internal/auth"
	"agy-gemini-web-mcp/internal/geminiweb"
)

const (
	scottyStartURL = "https://content-push.googleapis.com/upload/"
	scottyPushID   = "feeds/mcudyrk2a4khkz"
)

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
	reqStart.Header.Set("Push-Id", scottyPushID)
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
		uploadURL = respStart.Header.Get("x-goog-upload-url")
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
		// Fallback to synthetic ref with timestamp if not parsed
		uploadRef = fmt.Sprintf("/contrib_service/ttl_1d/%s", img.FileName)
	}

	return &geminiweb.ScottyAttachment{
		UploadRef: uploadRef,
		FileName:  img.FileName,
		MimeType:  img.MimeType,
	}, nil
}

func extractUploadRef(body string, hdr fhttp.Header) string {
	// 1. Try header
	if ref := hdr.Get("X-Goog-Upload-Status-Resource-Id"); ref != "" {
		return ref
	}

	// 2. Try JSON parse
	var parsed struct {
		Resource struct {
			URL string `json:"url"`
			ID  string `json:"id"`
		} `json:"resource"`
		UploadRef string `json:"upload_ref"`
		Session   struct {
			ResourceID string `json:"resourceId"`
		} `json:"sessionStatus"`
	}

	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		if parsed.Resource.URL != "" {
			return parsed.Resource.URL
		}
		if parsed.Resource.ID != "" {
			return parsed.Resource.ID
		}
		if parsed.UploadRef != "" {
			return parsed.UploadRef
		}
		if parsed.Session.ResourceID != "" {
			return parsed.Session.ResourceID
		}
	}

	// 3. String contains /contrib_service/ttl_1d/
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
