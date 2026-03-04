package oauth2

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	stateLen        = 16
	codeVerifierLen = 32
	stateCookieTTL  = 10 * time.Minute
)

// generateRandom returns a cryptographically random hex string of byteLen bytes.
func generateRandom(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// statePayload is stored in the state cookie during the OAuth flow.
type statePayload struct {
	State        string
	CodeVerifier string
}

// setStateCookie stores the state and code verifier in a short-lived cookie.
func (m *Manager) setStateCookie(w http.ResponseWriter, payload statePayload) {
	// We encode state:codeVerifier as a simple joined value.
	// In production you may want to sign this cookie.
	value := payload.State + ":" + payload.CodeVerifier

	http.SetCookie(w, &http.Cookie{
		Name:     m.opts.stateCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(stateCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.opts.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// stateFromRequest reads and validates the state cookie, returning the payload.
func (m *Manager) stateFromRequest(r *http.Request, returnedState string) (statePayload, bool) {
	cookie, err := r.Cookie(m.opts.stateCookieName)
	if err != nil {
		return statePayload{}, false
	}

	// Split "state:codeVerifier"
	parts := splitN(cookie.Value, ":", 2)
	if len(parts) != 2 {
		return statePayload{}, false
	}

	if parts[0] != returnedState {
		return statePayload{}, false
	}

	return statePayload{
		State:        parts[0],
		CodeVerifier: parts[1],
	}, true
}

// clearStateCookie removes the state cookie after the flow completes.
func (m *Manager) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.opts.stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.opts.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// splitN splits s by sep into at most n substrings.
func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for range n - 1 {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}

		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}

	result = append(result, s)

	return result
}

// indexOf returns the index of sep in s, or -1.
func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}

	return -1
}
