package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
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
				if err := r.RotateOnce(ctx); err != nil {
					log.Printf("[rotator] periodic cookie rotation error: %v", err)
				}
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
		log.Printf("[rotator] cookie store is empty, skipping rotation")
		return nil
	}

	// Payload for Google RotateCookies
	body := []byte(`[658, "-8492049182941029412"]`)

	req, err := fhttp.NewRequestWithContext(ctx, "POST", "https://accounts.google.com/RotateCookies", bytes.NewReader(body))
	if err != nil {
		log.Printf("[rotator] failed to create rotate request: %v", err)
		return fmt.Errorf("create rotate request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieHdr)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15")
	req.Header.Set("Origin", "https://gemini.google.com")
	req.Header.Set("Referer", "https://gemini.google.com/")

	resp, err := r.session.TLSClient().Do(req)
	if err != nil {
		log.Printf("[rotator] network error executing rotate request: %v", err)
		return fmt.Errorf("execute rotate request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// Check for expired session (HTTP 401 or 403)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		log.Printf("[rotator] WARNING: session expired (HTTP %d). Attempting AutoHarvest without spawning processes...", resp.StatusCode)
		if harvestErr := r.session.AutoHarvest(ctx); harvestErr != nil {
			log.Printf("[rotator] WARNING: AutoHarvest failed: %v. Please re-authenticate by starting Chrome with: open -a \"Google Chrome\" --args --remote-debugging-port=9222 and visiting https://gemini.google.com", harvestErr)
			return fmt.Errorf("session expired (HTTP %d) and cookies could not be harvested: %w", resp.StatusCode, harvestErr)
		}
		log.Printf("[rotator] AutoHarvest succeeded after expired session (HTTP %d)", resp.StatusCode)
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[rotator] unexpected status code from RotateCookies: %d", resp.StatusCode)
		return fmt.Errorf("unexpected status code from RotateCookies: %d", resp.StatusCode)
	}

	var updated bool
	for _, c := range resp.Cookies() {
		if c.Value != "" {
			store.Set(c.Name, c.Value)
			updated = true
		}
	}

	if updated {
		if err := store.Save(); err != nil {
			log.Printf("[rotator] failed to save rotated cookies: %v", err)
			return fmt.Errorf("save rotated cookies: %w", err)
		}
		log.Printf("[rotator] cookies successfully rotated and updated")
	} else {
		log.Printf("[rotator] cookie rotation check succeeded (no new cookies in response)")
	}

	return nil
}
