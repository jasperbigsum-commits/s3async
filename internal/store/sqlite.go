package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/task"
	_ "github.com/mattn/go-sqlite3"
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
);
CREATE TABLE IF NOT EXISTS task_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    path TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    size INTEGER NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(task_id) REFERENCES tasks(id)
);`

	if _, err := r.db.Exec(query); err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) Create(t task.Task, items []task.Item) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin create transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
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

	for _, item := range items {
		_, err = tx.Exec(
			`INSERT INTO task_items (task_id, path, relative_path, size, status, error_message, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			item.TaskID,
			item.Path,
			item.RelativePath,
			item.Size,
			string(item.Status),
			item.Error,
			item.CreatedAt.Format(time.RFC3339Nano),
			item.UpdatedAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert task item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create transaction: %w", err)
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

func (r *SQLiteTaskRepository) ListItems(taskID string) ([]task.Item, error) {
	rows, err := r.db.Query(`SELECT task_id, path, relative_path, size, status, error_message, created_at, updated_at FROM task_items WHERE task_id = ? ORDER BY id ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query task items: %w", err)
	}
	defer rows.Close()

	var items []task.Item
	for rows.Next() {
		item, err := scanTaskItem(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan task item row: %w", err)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task item rows: %w", err)
	}

	return items, nil
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

func scanTaskItem(scan scanFn) (task.Item, error) {
	var item task.Item
	var status string
	var createdAt string
	var updatedAt string
	if err := scan(&item.TaskID, &item.Path, &item.RelativePath, &item.Size, &status, &item.Error, &createdAt, &updatedAt); err != nil {
		return task.Item{}, err
	}

	item.Status = task.ItemStatus(status)
	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return task.Item{}, fmt.Errorf("parse task_item created_at: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return task.Item{}, fmt.Errorf("parse task_item updated_at: %w", err)
	}
	item.CreatedAt = parsedCreatedAt
	item.UpdatedAt = parsedUpdatedAt
	return item, nil
}
