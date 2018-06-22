package share_manager_owncloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cernbox/reva/api"

	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags/zap"
	"go.uber.org/zap"
)

func New(dbUsername, dbPassword, dbHost string, dbPort int, dbName string, vfs api.VirtualStorage) (api.ShareManager, error) {
	fmt.Println(dbUsername)
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", dbUsername, dbPassword, dbHost, dbPort, dbName))
	if err != nil {
		return nil, err
	}

	return &shareManager{db: db, vfs: vfs}, nil
}

type shareManager struct {
	db  *sql.DB
	vfs api.VirtualStorage
}

func (sm *shareManager) ListFolderShares(ctx context.Context) ([]*api.FolderShare, error) {
	l := ctx_zap.Extract(ctx)
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	dbShares, err := sm.getDBShares(ctx, u.AccountId)
	if err != nil {
		return nil, err
	}
	shares := []*api.FolderShare{}
	for _, dbShare := range dbShares {
		share, err := sm.convertToFolderShare(ctx, dbShare)
		if err != nil {
			l.Error("", zap.Error(err))
			//TODO(labkode): log error and continue
			continue
		}
		shares = append(shares, share)

	}
	return shares, nil
}

func (sm *shareManager) UpdateFolderShare(ctx context.Context, id string, updateReadOnly, readOnly bool) (*api.FolderShare, error) {
	l := ctx_zap.Extract(ctx)
	u, err := getUserFromContext(ctx)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	share, err := sm.GetFolderShare(ctx, id)
	if err != nil {
		l.Error("error getting share before update", zap.Error(err))
		return nil, err
	}

	stmtString := "update oc_share set "
	stmtPairs := map[string]interface{}{}

	if updateReadOnly {
		if readOnly {
			stmtPairs["permissions"] = 1
		} else {
			stmtPairs["permissions"] = 15
		}
	}

	if len(stmtPairs) == 0 { // nothing to update
		return share, nil
	}

	stmtTail := []string{}
	stmtValues := []interface{}{}

	for k, v := range stmtPairs {
		stmtTail = append(stmtTail, k+"=?")
		stmtValues = append(stmtValues, v)
	}

	stmtString += strings.Join(stmtTail, ",") + " where uid_owner=? and id=?"
	stmtValues = append(stmtValues, u.AccountId, id)

	stmt, err := sm.db.Prepare(stmtString)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	_, err = stmt.Exec(stmtValues...)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	l.Info("updated oc share")

	share, err = sm.GetFolderShare(ctx, id)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	return share, nil
}
func (sm *shareManager) Unshare(ctx context.Context, id string) error {
	l := ctx_zap.Extract(ctx)
	u, err := getUserFromContext(ctx)
	if err != nil {
		l.Error("", zap.Error(err))
		return err
	}

	stmt, err := sm.db.Prepare("delete from oc_share where uid_owner=? and id=?")
	if err != nil {
		l.Error("", zap.Error(err))
		return err
	}

	res, err := stmt.Exec(u.AccountId, id)
	if err != nil {
		l.Error("", zap.Error(err))
		return err
	}

	rowCnt, err := res.RowsAffected()
	if err != nil {
		l.Error("", zap.Error(err))
		return err
	}

	if rowCnt == 0 {
		err := api.NewError(api.PublicLinkNotFoundErrorCode)
		l.Error("", zap.Error(err), zap.String("id", id))
		return err
	}
	return nil
}

func (sm *shareManager) GetFolderShare(ctx context.Context, id string) (*api.FolderShare, error) {
	l := ctx_zap.Extract(ctx)
	u, err := getUserFromContext(ctx)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	dbShare, err := sm.getDBShare(ctx, u.AccountId, id)
	if err != nil {
		l.Error("cannot get db share", zap.Error(err), zap.String("id", id))
		return nil, err
	}

	share, err := sm.convertToFolderShare(ctx, dbShare)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	return share, nil
}

func (sm *shareManager) AddFolderShare(ctx context.Context, path string, recipient *api.ShareRecipient, readOnly bool) (*api.FolderShare, error) {
	l := ctx_zap.Extract(ctx)
	u, err := getUserFromContext(ctx)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	md, err := sm.vfs.GetMetadata(ctx, path)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	itemType := "file"
	if md.IsDir {
		itemType = "folder"
	}
	permissions := 15
	if readOnly {
		permissions = 1
	}

	prefix, itemSource := splitFileID(md.Id)
	fileSource, err := strconv.ParseUint(itemSource, 10, 64)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	shareType := 0 // user
	if recipient.Type == api.ShareRecipient_GROUP {
		shareType = 1
	}

	stmtString := "insert into oc_share set share_type=?,uid_owner=?,uid_initiator=?,item_type=?,fileid_prefix=?,item_source=?,file_source=?,permissions=?,stime=?,share_with=?"
	stmtValues := []interface{}{shareType, u.AccountId, u.AccountId, itemType, prefix, itemSource, fileSource, permissions, time.Now().Unix(), recipient.Identity}

	stmt, err := sm.db.Prepare(stmtString)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}

	result, err := stmt.Exec(stmtValues...)
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	lastId, err := result.LastInsertId()
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	l.Info("created oc share", zap.Int64("share_id", lastId))

	share, err := sm.GetFolderShare(ctx, fmt.Sprintf("%d", lastId))
	if err != nil {
		l.Error("", zap.Error(err))
		return nil, err
	}
	return share, nil
}

/*
type ocShare struct {
	ID          int64          `db:"id"`
	ShareType   int            `db:"share_type"`
	ShareWith   sql.NullString `db:"share_with"`
	UIDOwner    string         `db:"uid_owner"`
	Parent      sql.NullInt64  `db:"parent"`
	ItemType    sql.NullString `db:"item_type"`
	ItemSource  sql.NullString `db:"item_source"`
	ItemTarget  sql.NullString `db:"item_target"`
	FileSource  sql.NullInt64  `db:"file_source"`
	FileTarget  sql.NullString `db:"file_target"`
	Permissions string         `db:"permissions"`
	STime       int            `db:"stime"`
	Accepted    int            `db:"accepted"`
	Expiration  time.Time      `db:"expiration"`
	Token       sql.NullString `db:"token"`
	MailSend    int            `db:"mail_send"`
}
*/

type dbShare struct {
	ID          int
	Prefix      string
	ItemSource  string
	ShareWith   string
	Permissions int
	ShareType   int
	STime       int
}

func (sm *shareManager) getDBShare(ctx context.Context, accountID, id string) (*dbShare, error) {
	l := ctx_zap.Extract(ctx)
	intID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		l.Error("cannot parse id to int64", zap.Error(err))
		return nil, err
	}

	var (
		shareWith   string
		prefix      string
		itemSource  string
		shareType   int
		stime       int
		permissions int
	)

	query := "select coalesce(share_with, '') as share_with, coalesce(fileid_prefix, '') as fileid_prefix, coalesce(item_source, '') as item_source, stime, permissions, share_type from oc_share where uid_owner=? and id=?"
	if err := sm.db.QueryRow(query, accountID, id).Scan(&shareWith, &prefix, &itemSource, &stime, &permissions, &shareType); err != nil {
		if err == sql.ErrNoRows {
			return nil, api.NewError(api.FolderShareNotFoundErrorCode)
		}
		return nil, err
	}
	dbShare := &dbShare{ID: int(intID), Prefix: prefix, ItemSource: itemSource, ShareWith: shareWith, STime: stime, Permissions: permissions, ShareType: shareType}
	return dbShare, nil

}
func (sm *shareManager) getDBShares(ctx context.Context, accountID string) ([]*dbShare, error) {
	query := "select id, coalesce(share_with, '') as share_with, coalesce(fileid_prefix, '') as fileid_prefix, coalesce(item_source, '') as item_source, stime, permissions, share_type from oc_share where uid_owner=? and (share_type=? or share_type=?) "
	rows, err := sm.db.Query(query, accountID, 0, 1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		id          int
		shareWith   string
		prefix      string
		itemSource  string
		shareType   int
		stime       int
		permissions int
	)

	dbShares := []*dbShare{}
	for rows.Next() {
		err := rows.Scan(&id, &shareWith, &prefix, &itemSource, &stime, &permissions, &shareType)
		if err != nil {
			return nil, err
		}
		dbShare := &dbShare{ID: id, Prefix: prefix, ItemSource: itemSource, ShareWith: shareWith, STime: stime, Permissions: permissions, ShareType: shareType}
		dbShares = append(dbShares, dbShare)

	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return dbShares, nil
}

func (sm *shareManager) convertToFolderShare(ctx context.Context, dbShare *dbShare) (*api.FolderShare, error) {
	var recipientType api.ShareRecipient_RecipientType
	if dbShare.ShareType == 0 {
		recipientType = api.ShareRecipient_USER
	} else {
		recipientType = api.ShareRecipient_GROUP
	}
	path := joinFileID(dbShare.Prefix, dbShare.ItemSource)
	share := &api.FolderShare{
		Id:       fmt.Sprintf("%d", dbShare.ID),
		Mtime:    uint64(dbShare.STime),
		Path:     path,
		ReadOnly: dbShare.Permissions == 1,
		Recipient: &api.ShareRecipient{
			Identity: dbShare.ShareWith,
			Type:     recipientType,
		},
	}
	return share, nil

}

func getUserFromContext(ctx context.Context) (*api.User, error) {
	u, ok := api.ContextGetUser(ctx)
	if !ok {
		return nil, api.NewError(api.ContextUserRequiredError)
	}
	return u, nil
}

// getFileIDParts returns the two parts of a fileID.
// A fileID like home:1234 will be separated into the prefix (home) and the inode(1234).
func splitFileID(fileID string) (string, string) {
	tokens := strings.Split(fileID, ":")
	return tokens[0], tokens[1]
}

// joinFileID concatenates the prefix and the inode to form a valid fileID.
func joinFileID(prefix, inode string) string {
	return strings.Join([]string{prefix, inode}, ":")
}