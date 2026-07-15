package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"tokenhub/internal/models"
)

type Store struct {
	db     *gorm.DB
	driver string
}

type OpenConfig struct {
	Driver string
	DSN    string
}

func Open(ctx context.Context, cfg OpenConfig) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "sqlite"
	}

	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		if driver == "sqlite" {
			dsn = "var/db/app.sqlite"
		} else {
			return nil, errors.New("dsn is required")
		}
	}

	if driver == "sqlite" {
		if err := ensureSQLiteDir(dsn); err != nil {
			return nil, err
		}
	}

	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		dialector = sqlite.Open(dsn)
	case "postgres", "pgx":
		dialector = postgres.Open(dsn)
	default:
		return nil, errors.New("unsupported db driver")
	}

	gdb, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}

	s := &Store{db: gdb, driver: driver}
	if err := s.Migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(&models.SystemConfig{}, &models.User{}); err != nil {
		return err
	}

	_, err := s.GetSystemConfig(ctx)
	return err
}

func (s *Store) GetSystemConfig(ctx context.Context) (models.SystemConfig, error) {
	var cfg models.SystemConfig
	err := s.db.WithContext(ctx).First(&cfg, 1).Error
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.SystemConfig{}, err
	}

	cfg = models.DefaultSystemConfig()
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&cfg).Error; err != nil {
		return models.SystemConfig{}, err
	}

	if err := s.db.WithContext(ctx).First(&cfg, 1).Error; err != nil {
		return models.SystemConfig{}, err
	}
	return cfg, nil
}

type SystemConfigUpdate struct {
	WarnText *string
}

func (s *Store) UpdateSystemConfig(ctx context.Context, u SystemConfigUpdate) (models.SystemConfig, error) {
	cfg, err := s.GetSystemConfig(ctx)
	if err != nil {
		return models.SystemConfig{}, err
	}

	if u.WarnText != nil {
		cfg.WarnText = strings.TrimSpace(*u.WarnText)
	}

	cfg.UpdatedAtUTC = time.Now().UTC()
	if err := s.db.WithContext(ctx).Save(&cfg).Error; err != nil {
		return models.SystemConfig{}, err
	}
	return cfg, nil
}

func (s *Store) GetUserByNickname(ctx context.Context, nickname string) (models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).Where("nickname = ?", nickname).First(&user).Error
	return user, err
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	return user, err
}

func (s *Store) CreateUser(ctx context.Context, user models.User) (models.User, error) {
	_, err := s.GetUserByNickname(ctx, user.Nickname)
	if err == nil {
		return models.User{}, errors.New("nickname already exists")
	}

	err = s.db.WithContext(ctx).Create(&user).Error
	return user, err
}

func (s *Store) UpdateUserPassword(ctx context.Context, userID int64, password string) error {
	return s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Update("password", password).Error
}

func (s *Store) ListUsers(ctx context.Context) ([]models.User, error) {
	var users []models.User
	err := s.db.WithContext(ctx).Find(&users).Error
	return users, err
}

func (s *Store) CreateDefaultUser(ctx context.Context) error {
	_, err := s.GetUserByNickname(ctx, "admin")
	if err == nil {
		return nil
	}

	_, err = s.CreateUser(ctx, models.User{
		Nickname: "admin",
		Password: "admin",
	})
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	var count int64
	s.db.WithContext(ctx).Model(&models.User{}).Count(&count)
	if count <= 1 {
		return errors.New("cannot delete last user")
	}
	return s.db.WithContext(ctx).Delete(&models.User{}, id).Error
}

func ensureSQLiteDir(dsn string) error {
	path := strings.TrimSpace(dsn)
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSpace(path)
	if path == "" || path == ":memory:" {
		return nil
	}

	dir := filepath.Dir(path)
	if dir == "." || dir == "/" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return nil
}
