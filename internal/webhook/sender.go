package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
)

// The outbound send path (ADR-0041 decisions 2 and 6): one attempt, over
// the Guard-screened transport (scheme assertion, IP screening,
// connect-time DNS-rebinding pinning), redirection NEVER followed
// (CheckRedirect returns the last response — an open redirector cannot
// walk a hop the Guard never screened), response bodies read bounded.
// The T-364 dispatcher reuses this same per-attempt path with the retry/
// dead-letter state machine around it; the REST test endpoint calls it
// once, synchronously, with no outbox row (decision 2's "test = 同步直发").

// maxBodyRead bounds how much of a response body the records keep.
const maxBodyRead = 4 * 1024

// sendAttempt executes one delivery attempt of payload (the stored
// envelope bytes) under the handler spec. It returns the wire outcome;
// the caller decides persistence/troubleshooting.
func (b *Bus) sendAttempt(ctx context.Context, h *Handler, payload []byte, secret string) *attemptResult {
	start := b.now()
	res := &attemptResult{}
	done := func() { res.ElapsedMillis = b.now().Sub(start).Milliseconds() }

	if err := ValidateTargetURL(h.URL); err != nil {
		res.Error = err.Error()
		done()
		return res
	}
	if err := b.guard.CheckURL(ctx, h.URL); err != nil {
		res.Error = fmt.Sprintf("target rejected: %v", err)
		done()
		return res
	}

	body, contentType := payload, "application/json"
	method := http.MethodPost
	var hdrs []HeaderPair
	if h.Type == "custom-webhook" {
		rendered, err := renderCustomPayload(h, payload, b.decodableSecrets(h))
		if err != nil {
			res.Error = err.Error()
			done()
			return res
		}
		body = rendered
		method = h.Method
		hdrs = h.HTTPHeaders
		if contentTypeOf(hdrs) == "" {
			// The template's output is operator-defined; JSON is the
			// documented common case and the default only.
			contentType = "application/json"
		}
	} else {
		hdrs = h.CustomHTTPHeaders
	}

	req, err := http.NewRequestWithContext(ctx, method, h.URL, bytes.NewReader(body))
	if err != nil {
		res.Error = fmt.Sprintf("build request: %v", err)
		done()
		return res
	}
	req.Header.Set("Content-Type", contentType)
	for _, hp := range hdrs {
		if hp.Name != "" {
			req.Header.Set(hp.Name, hp.Value)
		}
	}
	if secret != "" {
		// The dual-state chain (webhook.md section 6): signing mode sends
		// the hex HMAC over the exact bytes on the wire; passthrough mode
		// sends the secret itself (the official default).
		if h.UseSecretForSigning {
			req.Header.Set(EventAuthHeader, Signature(secret, body))
		} else {
			req.Header.Set(EventAuthHeader, secret)
		}
	}

	resp, err := b.client().Do(req)
	if err != nil {
		res.Error = sanitizeSendError(err)
		done()
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyRead))
	res.StatusCode = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		res.Error = fmt.Sprintf("receiver answered %d", resp.StatusCode)
	}
	res.responseBody = string(snippet)
	res.responseHeaders = resp.Header.Clone()
	res.requestHeaders = req.Header.Clone()
	done()
	return res
}

// client is the no-follow, guarded transport (rebuilt per attempt — the
// transport is cheap and the Guard's dial closure is stateless). The
// client Timeout is the whole-request bound of webhook.md 5.2's
// timeoutMillis (connection + exchange + body read); since T-364 it is
// the configurable attempt timeout (official default 30s), not the
// interim 10s the ADR sketched before the anchor backfill.
func (b *Bus) client() *http.Client {
	return &http.Client{
		Timeout: b.attemptTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           b.guard.Dialer(b.attemptTimeout),
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: b.attemptTimeout,
		},
	}
}

// sanitizeSendError strips the wrapped url.Error's full target echo when
// it carries userinfo (defense in depth — URLs are pre-validated) and
// keeps the error class readable.
func sanitizeSendError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "://"); i >= 0 {
		if j := strings.Index(msg[i:], "@"); j >= 0 {
			msg = msg[:i+3] + msg[i+j+1:]
		}
	}
	return "send failed: " + msg
}

func contentTypeOf(hdrs []HeaderPair) string {
	for _, h := range hdrs {
		if strings.EqualFold(h.Name, "Content-Type") {
			return h.Value
		}
	}
	return ""
}

// renderCustomPayload renders the custom-webhook template (webhook.md
// 2.3's Go {{.}} placeholders). The context mirrors the envelope:
// domain/event_type/subscription_key/jpd_origin/source, the data fields
// ALSO surfaced at the top level (the spec's own examples mix {{.path}}
// with {{ .userContext.id }}), and {{.secrets.<name>}} for the named
// secrets.
func renderCustomPayload(h *Handler, payload []byte, secrets map[string]string) ([]byte, error) {
	if strings.TrimSpace(h.Payload) == "" {
		return payload, nil // no template: the standard envelope
	}
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("webhook: custom payload: %w", err)
	}
	ctxData := map[string]any{}
	for k, v := range env.Data {
		ctxData[k] = v
	}
	ctxData["domain"] = env.Domain
	ctxData["event_type"] = env.EventType
	ctxData["subscription_key"] = env.SubscriptionKey
	ctxData["jpd_origin"] = env.JPDOrigin
	ctxData["source"] = env.Source
	ctxData["userContext"] = map[string]any{
		"id":      env.UserContext.ID,
		"isToken": env.UserContext.IsToken,
		"realm":   env.UserContext.Realm,
	}
	ctxData["secrets"] = secrets
	tpl, err := template.New("payload").Parse(h.Payload)
	if err != nil {
		return nil, fmt.Errorf("webhook: payload template: %w", err)
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, ctxData); err != nil {
		return nil, fmt.Errorf("webhook: payload render: %w", err)
	}
	return out.Bytes(), nil
}

// decodableSecrets decrypts the named secrets for template injection;
// undecodable names inject empty strings (never the ciphertext).
func (b *Bus) decodableSecrets(h *Handler) map[string]string {
	out := map[string]string{}
	for name, enc := range h.Secrets {
		if b.cipher == nil || enc == "" {
			out[name] = ""
			continue
		}
		if plain, _, err := b.cipher.Decrypt(enc); err == nil {
			out[name] = plain
		} else {
			out[name] = ""
		}
	}
	return out
}
