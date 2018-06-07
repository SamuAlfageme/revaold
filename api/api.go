package api

import (
	"context"
	"io"
)

type key int

const (
	userKey  key = 0
	tokenKey key = 1
)

func ContextGetUser(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userKey).(*User)
	return u, ok
}

func ContextSetUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func ContextGetAccessToken(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenKey).(string)
	return t, ok
}

func ContextSetAccessToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

type MountOptions struct {
	ReadOnly bool
}

// Mount contains the information about a mount.
// Similar to "struct mntent" in /usr/include/mntent.h.
// See also getent(8).
// A Mount exposes two mount points, one path based and another namespace based.
// A path-based mount point can be '/home', a namespaced mount-point can be 'home:1234'
type Mount interface {
	Storage
	GetMountPoint() string
	GetMountPointId() string
	GetMountOptions() *MountOptions
}

type MountTable struct {
	Mounts []*MountTableEntry `json:"mounts"`
}

type MountTableEntry struct {
	MountPoint      string            `json:"mount_point"`
	MountID         string            `json:"mount_id"`
	MountOptions    *MountOptions     `json:"mount_options"`
	StorageDriver   string            `json:"storage_driver"`
	StorageOptions  interface{}       `json:"storage_options"`
	StorageWrappers []*StorageWrapper `json:"storage_wrappers"`
}

type StorageWrapper struct {
	Priority int         `json:"priority"`
	Name     string      `json:"name"`
	Options  interface{} `json:"options"`
}

// A VirtualStorage is similar to the
// Linux VFS (Virtual File Switch).
type VirtualStorage interface {
	AddMount(ctx context.Context, mount Mount) error
	RemoveMount(ctx context.Context, mountPoint string) error
	ListMounts(ctx context.Context) ([]Mount, error)
	GetMount(path string) (Mount, error)
	Storage
}

type Storage interface {
	CreateDir(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error
	Move(ctx context.Context, oldName, newName string) error
	GetMetadata(ctx context.Context, name string) (*Metadata, error)
	ListFolder(ctx context.Context, name string) ([]*Metadata, error)
	Upload(ctx context.Context, name string, r io.ReadCloser) error
	Download(ctx context.Context, name string) (io.ReadCloser, error)
	ListRevisions(ctx context.Context, path string) ([]*Revision, error)
	DownloadRevision(ctx context.Context, path, revisionKey string) (io.ReadCloser, error)
	RestoreRevision(ctx context.Context, path, revisionKey string) error
	ListRecycle(ctx context.Context, path string) ([]*RecycleEntry, error)
	RestoreRecycleEntry(ctx context.Context, restoreKey string) error
	EmptyRecycle(ctx context.Context, path string) error
	GetPathByID(ctx context.Context, id string) (string, error)
}

type PublicLinkOptions struct {
	Password         string
	ReadOnly         bool
	Expiration       uint64
	UpdatePassword   bool
	UpdateReadOnly   bool
	UpdateExpiration bool
}

type PublicLinkManager interface {
	CreatePublicLink(ctx context.Context, path string, opt *PublicLinkOptions) (*PublicLink, error)
	UpdatePublicLink(ctx context.Context, token string, opt *PublicLinkOptions) (*PublicLink, error)
	InspectPublicLink(ctx context.Context, token string) (*PublicLink, error)
	ListPublicLinks(ctx context.Context) ([]*PublicLink, error)
	RevokePublicLink(ctx context.Context, token string) error
}

type ShareManager interface {
	AddFolderShare(ctx context.Context, path, recipient string, readOnly bool) (*FolderShare, error)
	UpdateFolderShare(ctx context.Context, shareID string, readOnly bool) (*FolderShare, error)
	ListFolderShares(ctx context.Context) ([]*FolderShare, error)
	GetFolderShare(ctx context.Context, shareID string) (*FolderShare, error)
	Unshare(ctx context.Context, shareID string) error
	ListFolderMembers(ctx context.Context, path string) ([]string, error)
	GetFolderSharesInPath(ctx context.Context, path string) ([]*FolderShare, error)

	ListReceivedShares(ctx context.Context) ([]*FolderShare, error)
	MountReceivedShare(ctx context.Context, shareID string) error
	UnmountReceivedShare(ctx context.Context, shareID string) error
}

type AuthManager interface {
	Authenticate(ctx context.Context, clientID, clientPassword string) (*User, error)
}

type TokenManager interface {
	ForgeToken(ctx context.Context, user *User) (string, error)
	VerifyToken(ctx context.Context, token string) (*User, error)
}

func GetStatus(err error) StatusCode {
	if err == nil {
		return StatusCode_OK
	}

	appError, ok := err.(AppError)
	if !ok {
		return StatusCode_UNKNOWN
	}

	switch appError.Code {
	case StorageNotFoundErrorCode:
		return StatusCode_STORAGE_NOT_FOUND
	case StorageAlreadyExistsErrorCode:
		return StatusCode_STORAGE_ALREADY_EXISTS
	case StorageNotSupportedErrorCode:
		return StatusCode_STORAGE_NOT_SUPPORTED
	case StoragePermissionDeniedErrorCode:
		return StatusCode_STORAGE_PERMISSIONDENIED
	case TokenInvalidErrorCode:
		return StatusCode_TOKEN_INVALID
	case UserNotFoundErrorCode:
		return StatusCode_USER_NOT_FOUND
	case PathInvalidError:
		return StatusCode_PATH_INVALID
	case ContextUserRequiredError:
		return StatusCode_CONTEXT_USER_REQUIRED
	case PublicLinkInvalidExpireDateErrorCode:
		return StatusCode_PUBLIC_LINK_INVALID_DATE
	case PublicLinkNotFoundErrorCode:
		return StatusCode_PUBLIC_LINK_NOT_FOUND
	default:
		return StatusCode_UNKNOWN
	}
}
