package dnstemplate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

// ── mock store ────────────────────────────────────────────────────────────────

type mockStore struct {
	rows   map[int64]*postgres.DNSRecordTemplate
	nextID int64
}

func newMockStore() *mockStore { return &mockStore{rows: map[int64]*postgres.DNSRecordTemplate{}} }

func (m *mockStore) Create(_ context.Context, name string, desc *string, recs, vars json.RawMessage) (*postgres.DNSRecordTemplate, error) {
	m.nextID++
	t := &postgres.DNSRecordTemplate{ID: m.nextID, Name: name, Description: desc, Records: recs, Variables: vars}
	m.rows[m.nextID] = t
	return t, nil
}
func (m *mockStore) GetByID(_ context.Context, id int64) (*postgres.DNSRecordTemplate, error) {
	t, ok := m.rows[id]
	if !ok {
		return nil, postgres.ErrDNSTemplateNotFound
	}
	return t, nil
}
func (m *mockStore) List(_ context.Context) ([]postgres.DNSRecordTemplate, error) {
	var out []postgres.DNSRecordTemplate
	for _, t := range m.rows {
		out = append(out, *t)
	}
	return out, nil
}
func (m *mockStore) Update(_ context.Context, id int64, name string, desc *string, recs, vars json.RawMessage) (*postgres.DNSRecordTemplate, error) {
	t, ok := m.rows[id]
	if !ok {
		return nil, postgres.ErrDNSTemplateNotFound
	}
	t.Name, t.Description, t.Records, t.Variables = name, desc, recs, vars
	return t, nil
}
func (m *mockStore) Delete(_ context.Context, id int64) error {
	if _, ok := m.rows[id]; !ok {
		return postgres.ErrDNSTemplateNotFound
	}
	delete(m.rows, id)
	return nil
}

func newSvc() *Service { return NewService(newMockStore(), zap.NewNop()) }

// ── ExtractVariables ──────────────────────────────────────────────────────────

func TestExtractVariables_Empty(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "A", Content: "1.2.3.4", TTL: 300},
	}
	vars := ExtractVariables(records)
	assert.Empty(t, vars)
}

func TestExtractVariables_SinglePlaceholder(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
	}
	vars := ExtractVariables(records)
	assert.Equal(t, map[string]string{"ip": ""}, vars)
}

func TestExtractVariables_MultiplePlaceholders(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "{{sub}}", Type: "CNAME", Content: "{{target}}", TTL: 300},
		{Name: "@", Type: "MX", Content: "{{mx_host}}", TTL: 300},
		{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
	}
	vars := ExtractVariables(records)
	assert.Len(t, vars, 4)
	assert.Contains(t, vars, "sub")
	assert.Contains(t, vars, "target")
	assert.Contains(t, vars, "mx_host")
	assert.Contains(t, vars, "ip")
}

func TestExtractVariables_Deduplicates(t *testing.T) {
	// Same {{ip}} used in two records — should appear once
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
		{Name: "www", Type: "A", Content: "{{ip}}", TTL: 300},
	}
	vars := ExtractVariables(records)
	assert.Len(t, vars, 1)
}

// ── substitute ────────────────────────────────────────────────────────────────

func TestSubstitute_Basic(t *testing.T) {
	out := substitute("{{ip}}", map[string]string{"ip": "1.2.3.4"})
	assert.Equal(t, "1.2.3.4", out)
}

func TestSubstitute_Multiple(t *testing.T) {
	out := substitute("{{sub}}.{{domain}}", map[string]string{"sub": "mail", "domain": "example.com"})
	assert.Equal(t, "mail.example.com", out)
}

func TestSubstitute_NoPlaceholder(t *testing.T) {
	out := substitute("static.content", map[string]string{"ip": "1.2.3.4"})
	assert.Equal(t, "static.content", out)
}

// ── renderRecords ─────────────────────────────────────────────────────────────

func TestRenderRecords_AllVariablesProvided(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "a", Content: "{{ip}}", TTL: 300},
		{Name: "www", Type: "cname", Content: "@", TTL: 300},
		{Name: "@", Type: "mx", Content: "{{mx}}", TTL: 300, Priority: 10},
	}
	vars := map[string]string{"ip": "1.2.3.4", "mx": "mail.example.com"}

	rendered, err := renderRecords(records, vars)
	require.NoError(t, err)
	assert.Len(t, rendered, 3)
	assert.Equal(t, "1.2.3.4", rendered[0].Content)
	assert.Equal(t, "A", rendered[0].Type)
	assert.Equal(t, "mail.example.com", rendered[2].Content)
	assert.Equal(t, 10, rendered[2].Priority)
}

func TestRenderRecords_MissingVariable(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
	}
	_, err := renderRecords(records, map[string]string{}) // ip not provided
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingVariable))
	assert.Contains(t, err.Error(), "ip")
}

func TestRenderRecords_NoVariablesNeeded(t *testing.T) {
	records := []postgres.TemplateRecord{
		{Name: "@", Type: "A", Content: "1.2.3.4", TTL: 300},
	}
	rendered, err := renderRecords(records, map[string]string{})
	require.NoError(t, err)
	assert.Len(t, rendered, 1)
	assert.Equal(t, "1.2.3.4", rendered[0].Content)
}

// ── Service.Create ────────────────────────────────────────────────────────────

func TestCreate_AutoExtractsVariables(t *testing.T) {
	svc := newSvc()
	t.Run("creates template and extracts vars", func(t *testing.T) {
		in := CreateInput{
			Name: "Standard Web",
			Records: []postgres.TemplateRecord{
				{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
				{Name: "www", Type: "CNAME", Content: "@", TTL: 300},
			},
		}
		tmpl, err := svc.Create(context.Background(), in)
		require.NoError(t, err)
		assert.Equal(t, "Standard Web", tmpl.Name)

		var vars map[string]string
		require.NoError(t, json.Unmarshal(tmpl.Variables, &vars))
		assert.Contains(t, vars, "ip")
	})
}

func TestCreate_EmptyName_Error(t *testing.T) {
	svc := newSvc()
	_, err := svc.Create(context.Background(), CreateInput{Name: "  "})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidTemplate))
}

// ── Service.ApplyTemplate ─────────────────────────────────────────────────────

func TestApplyTemplate_Success(t *testing.T) {
	svc := newSvc()
	tmpl, err := svc.Create(context.Background(), CreateInput{
		Name: "Email Setup",
		Records: []postgres.TemplateRecord{
			{Name: "@", Type: "MX", Content: "{{mx}}", TTL: 300, Priority: 10},
			{Name: "@", Type: "TXT", Content: "v=spf1 include:{{mx}} ~all", TTL: 300},
		},
	})
	require.NoError(t, err)

	rendered, err := svc.ApplyTemplate(context.Background(), tmpl.ID, map[string]string{"mx": "mail.example.com"})
	require.NoError(t, err)
	assert.Len(t, rendered, 2)
	assert.Equal(t, "mail.example.com", rendered[0].Content)
	assert.Equal(t, "v=spf1 include:mail.example.com ~all", rendered[1].Content)
}

func TestApplyTemplate_NotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.ApplyTemplate(context.Background(), 9999, map[string]string{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTemplateNotFound))
}

func TestApplyTemplate_MissingVariable(t *testing.T) {
	svc := newSvc()
	tmpl, _ := svc.Create(context.Background(), CreateInput{
		Name: "T",
		Records: []postgres.TemplateRecord{
			{Name: "@", Type: "A", Content: "{{ip}}", TTL: 300},
		},
	})

	_, err := svc.ApplyTemplate(context.Background(), tmpl.ID, map[string]string{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingVariable))
}

// ── Service.Delete ────────────────────────────────────────────────────────────

func TestDelete_NotFound(t *testing.T) {
	svc := newSvc()
	err := svc.Delete(context.Background(), 9999)
	assert.True(t, errors.Is(err, ErrTemplateNotFound))
}

func TestDelete_Success(t *testing.T) {
	svc := newSvc()
	tmpl, _ := svc.Create(context.Background(), CreateInput{Name: "T", Records: nil})
	err := svc.Delete(context.Background(), tmpl.ID)
	require.NoError(t, err)
	_, err = svc.Get(context.Background(), tmpl.ID)
	assert.True(t, errors.Is(err, ErrTemplateNotFound))
}
