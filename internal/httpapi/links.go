package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/JP0722/shortly/internal/link"
)

const (
	// A create request is a tiny JSON object. Anything larger is a mistake or an
	// attack, and decoding an unbounded body is a trivial way to exhaust memory.
	maxCreateBodyBytes = 4 << 10 // 4 KiB

	// How many fresh codes to try before giving up. Collisions are rare, so
	// needing more than one attempt already means something is wrong.
	maxCodeAttempts = 5
)

// The `json:"..."` strings are struct tags: metadata telling encoding/json which
// JSON key maps to which Go field. Without them it would use the Go field names.
type createLinkRequest struct {
	URL string `json:"url"`
}

type createLinkResponse struct {
	Code      string `json:"code"`
	ShortURL  string `json:"short_url"`
	TargetURL string `json:"target_url"`
}

// handleCreateLink handles POST /v1/links.
func (a *API) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	// MaxBytesReader caps the body and makes the decoder below fail once the
	// limit is passed, rather than reading gigabytes into memory first.
	r.Body = http.MaxBytesReader(w, r.Body, maxCreateBodyBytes)

	var req createLinkRequest

	dec := json.NewDecoder(r.Body)
	// Reject {"urll": "..."} instead of silently treating it as an empty URL.
	// A typo should be an error, not a mystery.
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON of the form {\"url\": \"https://...\"}")
		return
	}

	// Validation lives in the link package, not here. The HTTP layer decides the
	// status code; the domain decides what is valid.
	target, err := link.NormalizeURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Links created by an authenticated caller belong to them. Anonymous
	// creation still works and leaves user_id NULL.
	var ownerID int64
	if principal, ok := PrincipalFrom(r.Context()); ok {
		ownerID = principal.UserID
	}

	created, err := a.createWithRetry(r.Context(), target, ownerID)
	if err != nil {
		// Log the real reason for us, return a vague one to the client.
		a.logger.Error("create link failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}

	writeJSON(w, http.StatusCreated, createLinkResponse{
		Code:      created.Code,
		ShortURL:  a.baseURL + "/" + created.Code,
		TargetURL: created.TargetURL,
	})
}

// createWithRetry inserts a link, generating a new code if the first one is
// already taken.
//
// This is the other half of the UNIQUE index on links.code. We do not check
// whether a code exists before inserting — that would be a race. We insert, and
// let the database be the referee.
func (a *API) createWithRetry(ctx context.Context, target string, ownerID int64) (link.Link, error) {
	for attempt := range maxCodeAttempts {
		candidate := link.Link{Code: link.NewCode(), TargetURL: target, UserID: ownerID}

		err := a.store.Create(ctx, candidate)
		if err == nil {
			return candidate, nil
		}

		// Anything other than a collision is a real failure — stop immediately.
		if !errors.Is(err, link.ErrCodeTaken) {
			return link.Link{}, err
		}

		a.logger.Warn("short code collision, retrying", "attempt", attempt+1)
	}

	return link.Link{}, errors.New("exhausted attempts generating a unique code")
}

// handleRedirect handles GET /{code}.
func (a *API) handleRedirect(w http.ResponseWriter, r *http.Request) {
	// PathValue reads the {code} wildcard from the route pattern.
	code := r.PathValue("code")

	found, err := a.store.ByCode(r.Context(), code)

	if errors.Is(err, link.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such link")
		return
	}
	if err != nil {
		a.logger.Error("lookup link failed", "error", err, "code", code)
		writeError(w, http.StatusInternalServerError, "could not look up link")
		return
	}

	// 302 Found, not 301 Moved Permanently. Browsers cache a 301 more or less
	// forever, so the link could never be changed or deleted — and in Phase 3 we
	// would never see the click again to count it.
	http.Redirect(w, r, found.TargetURL, http.StatusFound)
}

type linkView struct {
	Code      string `json:"code"`
	ShortURL  string `json:"short_url"`
	TargetURL string `json:"target_url"`
	CreatedAt string `json:"created_at"`
}

const maxListLinks = 200

// handleListLinks handles GET /v1/links — the caller's own links only.
func (a *API) handleListLinks(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// The WHERE user_id = ? is the authorization. There is no code path that
	// could accidentally return someone else's rows.
	links, err := a.store.ListByUser(r.Context(), principal.UserID, maxListLinks)
	if err != nil {
		a.logger.Error("list links failed", "error", err, "user_id", principal.UserID)
		writeError(w, http.StatusInternalServerError, "could not list links")
		return
	}

	views := make([]linkView, 0, len(links))
	for _, l := range links {
		views = append(views, linkView{
			Code:      l.Code,
			ShortURL:  a.baseURL + "/" + l.Code,
			TargetURL: l.TargetURL,
			CreatedAt: l.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"links": views})
}

// handleDeleteLink handles DELETE /v1/links/{code}.
func (a *API) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	err := a.store.DeleteByCode(r.Context(), r.PathValue("code"), principal.UserID)
	if errors.Is(err, link.ErrNotFound) {
		// 404, not 403, for a link owned by somebody else. A 403 would confirm
		// the code exists, which lets an attacker enumerate valid links.
		writeError(w, http.StatusNotFound, "no such link")
		return
	}
	if err != nil {
		a.logger.Error("delete link failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not delete link")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
