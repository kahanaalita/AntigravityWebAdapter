package cdp

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CDPRequest represents a Chrome DevTools Protocol JSON-RPC request.
type CDPRequest struct {
	ID     uint64      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

// CDPResponse represents a Chrome DevTools Protocol JSON-RPC response.
type CDPResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *CDPError       `json:"error,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CDPError represents an error returned by CDP.
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *CDPError) Error() string {
	return fmt.Sprintf("CDP error %d: %s", e.Code, e.Message)
}

// Client wraps a WebSocket connection to a Chrome DevTools page target.
type Client struct {
	ws        *WSConn
	reqID     uint64
	mu        sync.Mutex
	pending   map[uint64]chan *CDPResponse
	eventSubs map[string][]func(json.RawMessage)
	closed    bool
}

// NewClient connects to a CDP WebSocket URL and initializes message routing.
func NewClient(wsURL string) (*Client, error) {
	ws, err := DialWS(wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to dial CDP websocket: %w", err)
	}

	c := &Client{
		ws:        ws,
		pending:   make(map[uint64]chan *CDPResponse),
		eventSubs: make(map[string][]func(json.RawMessage)),
	}

	ws.SetHandlers(c.handleMessage, c.handleError)
	ws.StartReadLoop()

	return c, nil
}

func (c *Client) handleMessage(data []byte) {
	var resp CDPResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	if resp.ID != 0 {
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()

		if ok && ch != nil {
			ch <- &resp
		}
		return
	}

	// Event handling
	if resp.Method != "" {
		c.mu.Lock()
		handlers := c.eventSubs[resp.Method]
		c.mu.Unlock()

		for _, h := range handlers {
			go h(resp.Params)
		}
	}
}

func (c *Client) handleError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}

// Call executes a CDP command and waits for the result with a default timeout.
func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	return c.CallWithTimeout(method, params, 30*time.Second)
}

// CallWithTimeout executes a CDP command with custom timeout.
func (c *Client) CallWithTimeout(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	id := atomic.AddUint64(&c.reqID, 1)

	req := CDPRequest{
		ID:     id,
		Method: method,
		Params: params,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cdp request: %w", err)
	}

	respCh := make(chan *CDPResponse, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("cdp client is closed")
	}
	c.pending[id] = respCh
	c.mu.Unlock()

	if err := c.ws.SendText(string(payload)); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to send cdp command %s: %w", method, err)
	}

	select {
	case resp, ok := <-respCh:
		if !ok || resp == nil {
			return nil, errors.New("cdp client connection dropped while waiting for response")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp command %s timed out after %v", method, timeout)
	}
}

// Evaluate runs a javascript expression in the page and returns the parsed value.
func (c *Client) Evaluate(expr string, awaitPromise bool) (interface{}, error) {
	raw, err := c.EvaluateRaw(expr, awaitPromise)
	if err != nil {
		return nil, err
	}

	var evalResult struct {
		Result struct {
			Type        string      `json:"type"`
			Value       interface{} `json:"value"`
			Description string      `json:"description"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}

	if err := json.Unmarshal(raw, &evalResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal eval result: %w", err)
	}

	if evalResult.ExceptionDetails != nil {
		msg := evalResult.ExceptionDetails.Text
		if evalResult.ExceptionDetails.Exception.Description != "" {
			msg += ": " + evalResult.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("javascript evaluation exception: %s", msg)
	}

	return evalResult.Result.Value, nil
}

// EvaluateRaw executes Runtime.evaluate and returns the raw JSON result.
func (c *Client) EvaluateRaw(expr string, awaitPromise bool) (json.RawMessage, error) {
	params := map[string]interface{}{
		"expression":    expr,
		"awaitPromise":  awaitPromise,
		"returnByValue": true,
	}
	return c.Call("Runtime.evaluate", params)
}

// Navigate tells the page to navigate to the specified URL.
func (c *Client) Navigate(url string) error {
	_, err := c.Call("Page.navigate", map[string]interface{}{
		"url": url,
	})
	return err
}

// Close closes the underlying WebSocket connection and terminates all pending calls.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.ws.Close()
}
