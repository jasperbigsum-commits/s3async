package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type SQLiteTaskRepository struct {
	db *sql.DB
}

func NewSQLiteTaskRepository(path string) (*SQLiteTaskRepository, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	repo := &SQLiteTaskRepository{db: db}
	if err := repo.migrate(); err != nil {
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}

	return repo, nil
}

func (r *SQLiteTaskRepository) migrate() error {
	query := `
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    bucket TEXT NOT NULL,
    prefix_value TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);`

	if _, err := r.db.Exec(query); err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) Create(t task.Task) error {
	_, err := r.db.Exec(
		`INSERT INTO tasks (id, source, bucket, prefix_value, mode, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID,
		t.Source,
		t.Bucket,
		t.Prefix,
		t.Mode,
		string(t.Status),
		t.CreatedAt.Format(time.RFC3339Nano),
		t.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) List() ([]task.Task, error) {
	rows, err := r.db.Query(`SELECT id, source, bucket, prefix_value, mode, status, created_at, updated_at FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []task.Task
	for rows.Next() {
		item, err := scanTask(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		tasks = append(tasks, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}

	return tasks, nil
}

func (r *SQLiteTaskRepository) Get(id string) (task.Task, error) {
	row := r.db.QueryRow(`SELECT id, source, bucket, prefix_value, mode, status, created_at, updated_at FROM tasks WHERE id = ?`, id)
	item, err := scanTask(row.Scan)
	if err != nil {
		return task.Task{}, fmt.Errorf("scan task by id: %w", err)
	}

	return item, nil
}

func (r *SQLiteTaskRepository) UpdateStatus(id string, status task.Status) error {
	_, err := r.db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`, string(status), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update task status row: %w", err)
	}

	return nil
}

type scanFn func(dest ...any) error

func scanTask(scan scanFn) (task.Task, error) {
	var item task.Task
	var status string
	var createdAt string
	var updatedAt string
	if err := scan(&item.ID, &item.Source, &item.Bucket, &item.Prefix, &item.Mode, &status, &createdAt, &updatedAt); err != nil {
		return task.Task{}, err
	}

	item.Status = task.Status(status)
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse updated_at: %w", err)
	}
	item.CreatedAt = parsedCreatedAt
	item.UpdatedAt = parsedUpdatedAt
	return item, nil
}
