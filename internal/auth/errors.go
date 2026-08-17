package auth

import "github.com/lzwzzy/binflow/internal/metadata"

// Sentinel aliases. The not-found errors below are the *same error values*
// metadata exports: callers may match either spelling through errors.Is,
// which is what the HTTP layer relies on to map revocation of an unknown
// token to the idempotent "Token not found" 200 (auth-model.md section
// 3.4 item 5). Wrapping must always target these aliases, never a private
// mirror — a mirror breaks errors.Is(err, metadata.ErrTokenNotFound).

// ErrTokenNotFound is the not-found sentinel of the token store.
var ErrTokenNotFound = metadata.ErrTokenNotFound

// ErrUserNotFound is the not-found sentinel of the user store.
var ErrUserNotFound = metadata.ErrUserNotFound
