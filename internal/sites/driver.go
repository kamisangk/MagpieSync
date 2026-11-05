package sites

import (
	"context"
	"sync"
	"time"

	"magpiesync/internal/media"
	"magpiesync/internal/store"
	syncpkg "magpiesync/internal/sync"
)

// AccountContext is the decrypted account material a driver needs to build a
// client. Credential is site-specific JSON.
type AccountContext struct {
	Account    *store.Account
	Credential []byte
	ProxyURL   string
	Rate       float64
	Store      *store.Store
}

type CookieInput struct {
	RawCookie string
	UserAgent string
	ProxyURL  string
	Rate      float64
}

type PasswordInput struct {
	Email    string
	Password string
	ProxyURL string
}

// LoginResult is the site-neutral result of a successful login. Credential is
// the plaintext site-specific payload that the HTTP layer encrypts.
type LoginResult struct {
	UID        string
	Name       string
	AvatarURL  string
	Credential []byte
	AutoSync   bool
}

// AuthErrorKind maps site errors to HTTP responses without importing net/http
// from adapter code.
type AuthErrorKind int

const (
	AuthBadRequest AuthErrorKind = iota
	AuthUnauthorized
	AuthBadGateway
	AuthUnavailable
	AuthTimeout
)

type AuthError struct {
	Kind    AuthErrorKind
	Message string
	Err     error
}

func (e *AuthError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "authentication failed"
}

func (e *AuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// QRStart is a generated login QR code and its polling session.
type QRStart struct {
	Key       string
	URL       string
	ExpiresAt time.Time
	Session   QRSession
}

type QRSession interface {
	Poll(ctx context.Context) (state string, result *LoginResult, err error)
	Close() error
}

// Driver owns all site-specific behavior: authentication, sync source and
// error classification. Optional capabilities are separate interfaces below.
type Driver interface {
	SiteID() string
	IsAuth(error) bool
	IsFatal(error) bool
	CookieLogin(ctx context.Context, in CookieInput) (*LoginResult, error)
	NewSource(ctx context.Context, in AccountContext) (syncpkg.Source, error)
}

type PasswordDriver interface {
	PasswordLogin(ctx context.Context, in PasswordInput) (*LoginResult, error)
}

type QRDriver interface {
	StartQR(ctx context.Context, proxyURL string, rate float64) (*QRStart, error)
}

type DirectOption struct {
	Quality int
	Label   string
	URL     string
	Headers map[string]string
}

type DirectResolver interface {
	Options(ctx context.Context, pageURL string) ([]DirectOption, error)
	Close() error
}

type DirectDriver interface {
	NewDirectResolver(ctx context.Context, in AccountContext) (DirectResolver, error)
}

type VideoClient interface {
	VideoDetail(ctx context.Context, remoteID string) (*media.VideoDetail, error)
	VideoTags(ctx context.Context, remoteID string) ([]string, error)
	PlayURL(ctx context.Context, remoteID string, partID int64, quality int) (*media.PlayInfo, error)
	Danmaku(ctx context.Context, partID int64, duration int) ([]media.Danmaku, error)
	Subtitles(ctx context.Context, remoteID string, partID int64) ([]media.Subtitle, error)
	SubtitleBody(ctx context.Context, rawURL string) ([]media.SubtitleItem, error)
}

type VideoDriver interface {
	NewVideoClient(ctx context.Context, in AccountContext) (VideoClient, error)
}

type Collection struct {
	Kind     string
	ID       string
	OwnerID  string
	Title    string
	CoverURL string
	Count    int
}

// CollectionPage is the result of resolving a user-supplied collection ref
// (an id, URL or site-specific compact form).
type CollectionPage struct {
	OwnerID string
	Items   []Collection
}

type CollectionsDriver interface {
	Collections(ctx context.Context, in AccountContext, ref string) (*CollectionPage, error)
}

// SourceRequest is the raw "add source" form input. Ref is free-form: each
// site decides whether it is a numeric id, a URL, or a site-defined compact ref.
type SourceRequest struct {
	Kind    string
	Ref     string
	OwnerID string
	ID      string
	Title   string
}

// SourceSpec is what a driver resolves the form input into: the folder to
// store locally and, optionally, a title to use when the user left it empty.
type SourceSpec struct {
	OwnerID  string
	RemoteID string
	Title    string
}

// SourceBuilder turns the shared dialog's input into a site-specific folder
// identity. Sites that do not implement it cannot add sources manually.
type SourceBuilder interface {
	BuildSource(req SourceRequest) (SourceSpec, error)
}

// TaskRequest bundles everything a site needs to expand synced items into
// download tasks; multipart sites have to call their API here.
type TaskRequest struct {
	Items       []store.Item
	SourceTitle string
	Quality     int
}

// TaskBuilder expands items into download tasks for sites whose downloads
// need extra remote metadata (for example, a multi-page video). The generic
// HTTP handler only knows how to enqueue the returned tasks.
type TaskBuilder interface {
	BuildTasks(ctx context.Context, in AccountContext, req TaskRequest) ([]store.DownloadTaskInput, error)
}

var (
	driverMu sync.RWMutex
	drivers  = map[string]Driver{}
)

// RegisterDriver is called from site registration packages. It panics on a
// duplicate id so build mistakes fail loudly at startup instead of silently
// selecting one adapter.
func RegisterDriver(id string, d Driver) {
	if id == "" || d == nil {
		panic("sites: invalid driver registration")
	}
	driverMu.Lock()
	defer driverMu.Unlock()
	if _, exists := drivers[id]; exists {
		panic("sites: duplicate driver for " + id)
	}
	drivers[id] = d
}

func DriverFor(id string) (Driver, bool) {
	driverMu.RLock()
	defer driverMu.RUnlock()
	d, ok := drivers[id]
	return d, ok
}

func IsAuth(err error) bool {
	driverMu.RLock()
	defer driverMu.RUnlock()
	for _, d := range drivers {
		if d.IsAuth(err) {
			return true
		}
	}
	return false
}

func IsFatal(err error) bool {
	driverMu.RLock()
	defer driverMu.RUnlock()
	for _, d := range drivers {
		if d.IsFatal(err) {
			return true
		}
	}
	return false
}
