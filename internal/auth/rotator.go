package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
)

// CookieRotator periodically pings accounts.google.com/RotateCookies to keep __Secure-1PSIDTS alive.
type CookieRotator struct {
	session  *SessionManager
	interval time.Duration
	stopCh   chan struct{}
}

// NewCookieRotator creates a rotator with a given interval (default 8 minutes).
func NewCookieRotator(session *SessionManager, interval time.Duration) *CookieRotator {
	if interval <= 0 {
		interval = 8 * time.Minute
	}
	return &CookieRotator{
		session:  session,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background rotation loop.
func (r *CookieRotator) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-ticker.C:
				_ = r.RotateOnce(ctx)
			}
		}
	}()
}

// Stop stops the background rotation loop.
func (r *CookieRotator) Stop() {
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
}

// RotateOnce sends a rotation request and stores any updated cookies.
func (r *CookieRotator) RotateOnce(ctx context.Context) error {
	store := r.session.Store()
	cookieHdr := store.CookieHeader()
	if cookieHdr == "" {
		return nil
	}

	// Payload for Google RotateCookies
	body := []byte(`[658, "-8492049182941029412"]`)

	req, err := fhttp.NewRequestWithContext(ctx, "POST", "https://accounts.google.com/RotateCookies", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create rotate request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHdr)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")

	resp, err := r.session.TLSClient().Do(req)
	if err != nil {
		return fmt.Errorf("execute rotate request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	var updated bool
	for _, c := range resp.Cookies() {
		if c.Value != "" {
			store.Set(c.Name, c.Value)
			updated = true
		}
	}

	if updated {
		_ = store.Save()
	}

	return nil
}
