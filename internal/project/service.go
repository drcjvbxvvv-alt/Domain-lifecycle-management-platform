package project

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"domain-platform/store/postgres"
)

var (
	ErrDuplicateSlug    = errors.New("project slug already exists")
	ErrDuplicateName    = errors.New("project name already exists")
	ErrInvalidSlug      = errors.New("invalid project slug")
	ErrPermissionDenied = errors.New("permission denied")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,98}[a-z0-9]$`)

type Service struct {
	store  *postgres.ProjectStore
	logger *zap.Logger
}

func NewService(store *postgres.ProjectStore, logger *zap.Logger) *Service {
	return &Service{store: store, logger: logger}
}

type CreateInput struct {
	Name        string
	Slug        string
	Description *string
	IsProd      bool
	OwnerID     *int64
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*postgres.Project, error) {
	if !slugPattern.MatchString(in.Slug) {
		return nil, ErrInvalidSlug
	}

	// Check for duplicate slug
	_, err := s.store.GetBySlug(ctx, in.Slug)
	if err == nil {
		return nil, ErrDuplicateSlug
	}
	if !errors.Is(err, postgres.ErrProjectNotFound) {
		return nil, fmt.Errorf("check slug: %w", err)
	}

	p := &postgres.Project{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		IsProd:      in.IsProd,
		OwnerID:     in.OwnerID,
	}

	created, err := s.store.Create(ctx, p)
	if err != nil {
		if strings.Contains(err.Error(), "uq_projects_name") {
			return nil, ErrDuplicateName
		}
		if strings.Contains(err.Error(), "uq_projects_slug") {
			return nil, ErrDuplicateSlug
		}
		return nil, fmt.Errorf("create project: %w", err)
	}

	s.logger.Info("project created",
		zap.Int64("id", created.ID),
		zap.String("slug", created.Slug),
	)
	return created, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (*postgres.Project, error) {
	return s.store.GetByID(ctx, id)
}

type ListInput struct {
	Cursor int64
	Limit  int
}

type ListResult struct {
	Items  []postgres.Project `json:"items"`
	Total  int64              `json:"total"`
	Cursor int64              `json:"cursor"`
}

func (s *Service) List(ctx context.Context, in ListInput) (*ListResult, error) {
	items, err := s.store.List(ctx, in.Cursor, in.Limit)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	total, err := s.store.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count projects: %w", err)
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}

	return &ListResult{
		Items:  items,
		Total:  total,
		Cursor: nextCursor,
	}, nil
}

type UpdateInput struct {
	ID          int64
	Name        string
	Slug        string
	Description *string
	IsProd      bool
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*postgres.Project, error) {
	if !slugPattern.MatchString(in.Slug) {
		return nil, ErrInvalidSlug
	}
	return s.store.Update(ctx, in.ID, in.Name, in.Slug, in.Description, in.IsProd)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.store.SoftDelete(ctx, id)
	if err != nil {
		return err
	}
	s.logger.Info("project deleted", zap.Int64("id", id))
	return nil
}
