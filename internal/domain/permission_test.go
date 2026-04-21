package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// ── mocks ─────────────────────────────────────────────────────────────────────

type mockPermStore struct {
	rows    map[string]*postgres.DomainPermission // key: "domainID:userID"
	listFn  func(ctx context.Context, domainID int64) ([]postgres.DomainPermissionWithUser, error)
	upsertFn func(ctx context.Context, domainID, userID int64, permission string, grantedBy *int64) error
	deleteFn func(ctx context.Context, domainID, userID int64) error
}

func (m *mockPermStore) key(d, u int64) string {
	return string(rune(d)) + ":" + string(rune(u))
}

func (m *mockPermStore) Upsert(ctx context.Context, domainID, userID int64, permission string, grantedBy *int64) error {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, domainID, userID, permission, grantedBy)
	}
	if m.rows == nil {
		m.rows = make(map[string]*postgres.DomainPermission)
	}
	m.rows[m.key(domainID, userID)] = &postgres.DomainPermission{
		DomainID: domainID, UserID: userID, Permission: permission, GrantedBy: grantedBy,
	}
	return nil
}

func (m *mockPermStore) Delete(ctx context.Context, domainID, userID int64) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, domainID, userID)
	}
	if m.rows == nil {
		return postgres.ErrDomainPermissionNotFound
	}
	k := m.key(domainID, userID)
	if _, ok := m.rows[k]; !ok {
		return postgres.ErrDomainPermissionNotFound
	}
	delete(m.rows, k)
	return nil
}

func (m *mockPermStore) Get(ctx context.Context, domainID, userID int64) (*postgres.DomainPermission, error) {
	if m.rows == nil {
		return nil, postgres.ErrDomainPermissionNotFound
	}
	p, ok := m.rows[m.key(domainID, userID)]
	if !ok {
		return nil, postgres.ErrDomainPermissionNotFound
	}
	return p, nil
}

func (m *mockPermStore) List(ctx context.Context, domainID int64) ([]postgres.DomainPermissionWithUser, error) {
	if m.listFn != nil {
		return m.listFn(ctx, domainID)
	}
	return nil, nil
}

type mockRoleStore struct {
	roles map[int64][]string
}

func (m *mockRoleStore) GetUserRoles(ctx context.Context, userID int64) ([]string, error) {
	return m.roles[userID], nil
}

func newTestSvc(perms PermissionStore, roles map[int64][]string) *PermissionService {
	return NewPermissionService(perms, &mockRoleStore{roles: roles}, zap.NewNop())
}

// ── GrantPermission ───────────────────────────────────────────────────────────

func TestGrantPermission_Valid(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, nil)

	err := svc.GrantPermission(context.Background(), 1, 2, PermEditor, 99)
	require.NoError(t, err)

	p, err := store.Get(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Equal(t, PermEditor, p.Permission)
}

func TestGrantPermission_InvalidLevel(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, nil)

	err := svc.GrantPermission(context.Background(), 1, 2, "superuser", 99)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid permission level")
}

// ── RevokePermission ──────────────────────────────────────────────────────────

func TestRevokePermission_Exists(t *testing.T) {
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 2, PermViewer, nil)

	svc := newTestSvc(store, nil)
	err := svc.RevokePermission(context.Background(), 1, 2)
	require.NoError(t, err)

	_, err = store.Get(context.Background(), 1, 2)
	assert.ErrorIs(t, err, postgres.ErrDomainPermissionNotFound)
}

func TestRevokePermission_NotFound(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, nil)

	err := svc.RevokePermission(context.Background(), 1, 99)
	require.Error(t, err)
	assert.True(t, errors.Is(err, postgres.ErrDomainPermissionNotFound))
}

// ── EffectivePermission ───────────────────────────────────────────────────────

func TestEffectivePermission_GlobalAdminAlwaysAdmin(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{
		5: {"admin"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermAdmin, eff)
}

func TestEffectivePermission_GlobalReleaseManagerIsAdmin(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{
		5: {"release_manager"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermAdmin, eff)
}

func TestEffectivePermission_GlobalOperatorIsEditor(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{
		5: {"operator"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermEditor, eff)
}

func TestEffectivePermission_GlobalViewerIsViewer(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{
		5: {"viewer"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermViewer, eff)
}

func TestEffectivePermission_DomainPermUpgradesGlobalRole(t *testing.T) {
	// User has global viewer, but domain-level editor → should get editor
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermEditor, nil)

	svc := newTestSvc(store, map[int64][]string{
		5: {"viewer"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermEditor, eff)
}

func TestEffectivePermission_GlobalRoleWinsOverDomainPerm(t *testing.T) {
	// User is global admin — domain-level viewer can't downgrade them
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermViewer, nil)

	svc := newTestSvc(store, map[int64][]string{
		5: {"admin"},
	})

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermAdmin, eff)
}

func TestEffectivePermission_NoRoleNoPerm_ReturnsEmpty(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{}) // no roles, no domain perms

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, "", eff)
}

func TestEffectivePermission_DirectDomainPermOnly(t *testing.T) {
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermAdmin, nil)

	svc := newTestSvc(store, map[int64][]string{5: {}}) // no global roles

	eff, err := svc.EffectivePermission(context.Background(), 1, 5)
	require.NoError(t, err)
	assert.Equal(t, PermAdmin, eff)
}

// ── HasPermission ─────────────────────────────────────────────────────────────

func TestHasPermission_EditorCanEdit(t *testing.T) {
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermEditor, nil)

	svc := newTestSvc(store, map[int64][]string{5: {}})

	ok, err := svc.HasPermission(context.Background(), 1, 5, PermEditor)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasPermission_EditorCannotApply(t *testing.T) {
	// editor < admin; apply requires admin
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermEditor, nil)

	svc := newTestSvc(store, map[int64][]string{5: {}})

	ok, err := svc.HasPermission(context.Background(), 1, 5, PermAdmin)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasPermission_ViewerCanView(t *testing.T) {
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermViewer, nil)

	svc := newTestSvc(store, map[int64][]string{5: {}})

	ok, err := svc.HasPermission(context.Background(), 1, 5, PermViewer)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasPermission_ViewerCannotEdit(t *testing.T) {
	store := &mockPermStore{}
	_ = store.Upsert(context.Background(), 1, 5, PermViewer, nil)

	svc := newTestSvc(store, map[int64][]string{5: {}})

	ok, err := svc.HasPermission(context.Background(), 1, 5, PermEditor)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasPermission_AdminCanDoAll(t *testing.T) {
	store := &mockPermStore{}
	svc := newTestSvc(store, map[int64][]string{5: {"admin"}})

	for _, level := range []string{PermViewer, PermEditor, PermAdmin} {
		ok, err := svc.HasPermission(context.Background(), 1, 5, level)
		require.NoError(t, err)
		assert.True(t, ok, "admin should satisfy level %s", level)
	}
}
