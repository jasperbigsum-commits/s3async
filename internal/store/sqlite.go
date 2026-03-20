package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
    total_items INTEGER NOT NULL DEFAULT 0,
    pending_items INTEGER NOT NULL DEFAULT 0,
    uploading_items INTEGER NOT NULL DEFAULT 0,
    success_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    skipped_items INTEGER NOT NULL DEFAULT 0,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    pending_bytes INTEGER NOT NULL DEFAULT 0,
    uploading_bytes INTEGER NOT NULL DEFAULT 0,
    success_bytes INTEGER NOT NULL DEFAULT 0,
    failed_bytes INTEGER NOT NULL DEFAULT 0,
    skipped_bytes INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT
);
CREATE TABLE IF NOT EXISTS task_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    path TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    size INTEGER NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    FOREIGN KEY(task_id) REFERENCES tasks(id)
);`

	if _, err := r.db.Exec(query); err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	taskColumns, err := r.tableColumns("tasks")
	if err != nil {
		return fmt.Errorf("load tasks columns: %w", err)
	}
	taskItemColumns, err := r.tableColumns("task_items")
	if err != nil {
		return fmt.Errorf("load task_items columns: %w", err)
	}

	alterStatements := []string{}
	if !taskColumns["total_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN total_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["pending_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN pending_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["uploading_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN uploading_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["success_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN success_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["failed_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN failed_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["skipped_items"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN skipped_items INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["total_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["pending_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN pending_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["uploading_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN uploading_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["success_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN success_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["failed_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN failed_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["skipped_bytes"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN skipped_bytes INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskColumns["last_error"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`)
	}
	if !taskColumns["started_at"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN started_at TEXT`)
	}
	if !taskColumns["completed_at"] {
		alterStatements = append(alterStatements, `ALTER TABLE tasks ADD COLUMN completed_at TEXT`)
	}
	if !taskItemColumns["attempt_count"] {
		alterStatements = append(alterStatements, `ALTER TABLE task_items ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0`)
	}
	if !taskItemColumns["started_at"] {
		alterStatements = append(alterStatements, `ALTER TABLE task_items ADD COLUMN started_at TEXT`)
	}
	if !taskItemColumns["completed_at"] {
		alterStatements = append(alterStatements, `ALTER TABLE task_items ADD COLUMN completed_at TEXT`)
	}

	for _, statement := range alterStatements {
		if _, err := r.db.Exec(statement); err != nil {
			return fmt.Errorf("apply migration statement %q: %w", statement, err)
		}
	}

	if err := r.refreshTaskSummaries(); err != nil {
		return fmt.Errorf("refresh task summaries: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) tableColumns(table string) (map[string]bool, error) {
	rows, err := r.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, fmt.Errorf("query table info for %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan table info for %s: %w", table, err)
		}
		columns[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table info for %s: %w", table, err)
	}
	return columns, nil
}

func (r *SQLiteTaskRepository) refreshTaskSummaries() error {
	rows, err := r.db.Query(`SELECT id FROM tasks`)
	if err != nil {
		return fmt.Errorf("query tasks for summary refresh: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan task id for summary refresh: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate task ids for summary refresh: %w", err)
	}

	for _, id := range ids {
		items, err := r.ListItems(id)
		if err != nil {
			return fmt.Errorf("list task items for %s summary refresh: %w", id, err)
		}
		t, err := r.Get(id)
		if err != nil {
			return fmt.Errorf("get task for %s summary refresh: %w", id, err)
		}
		total := task.BuildSummary(items)
		t.TotalItems = total.TotalItems
		t.PendingItems = total.PendingItems
		t.UploadingItems = total.UploadingItems
		t.SuccessItems = total.SuccessItems
		t.FailedItems = total.FailedItems
		t.SkippedItems = total.SkippedItems
		t.TotalBytes = total.TotalBytes
		t.PendingBytes = total.PendingBytes
		t.UploadingBytes = total.UploadingBytes
		t.SuccessBytes = total.SuccessBytes
		t.FailedBytes = total.FailedBytes
		t.SkippedBytes = total.SkippedBytes
		if t.LastError == "" {
			for i := len(items) - 1; i >= 0; i-- {
				if items[i].Error != "" {
					t.LastError = items[i].Error
					break
				}
			}
		}
		if err := r.UpdateTask(t); err != nil {
			return fmt.Errorf("update task for %s summary refresh: %w", id, err)
		}
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
		`INSERT INTO tasks (
			id, source, bucket, prefix_value, mode, status,
			total_items, pending_items, uploading_items, success_items, failed_items, skipped_items,
			total_bytes, pending_bytes, uploading_bytes, success_bytes, failed_bytes, skipped_bytes,
			last_error, created_at, updated_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID,
		t.Source,
		t.Bucket,
		t.Prefix,
		t.Mode,
		string(t.Status),
		t.TotalItems,
		t.PendingItems,
		t.UploadingItems,
		t.SuccessItems,
		t.FailedItems,
		t.SkippedItems,
		t.TotalBytes,
		t.PendingBytes,
		t.UploadingBytes,
		t.SuccessBytes,
		t.FailedBytes,
		t.SkippedBytes,
		t.LastError,
		t.CreatedAt.Format(time.RFC3339Nano),
		t.UpdatedAt.Format(time.RFC3339Nano),
		formatNullableTime(t.StartedAt),
		formatNullableTime(t.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}

	for _, item := range items {
		_, err = tx.Exec(
			`INSERT INTO task_items (
				task_id, path, relative_path, size, status, error_message, attempt_count,
				created_at, updated_at, started_at, completed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.TaskID,
			item.Path,
			item.RelativePath,
			item.Size,
			string(item.Status),
			item.Error,
			item.AttemptCount,
			item.CreatedAt.Format(time.RFC3339Nano),
			item.UpdatedAt.Format(time.RFC3339Nano),
			formatNullableTime(item.StartedAt),
			formatNullableTime(item.CompletedAt),
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
	rows, err := r.db.Query(`
		SELECT id, source, bucket, prefix_value, mode, status,
		       total_items, pending_items, uploading_items, success_items, failed_items, skipped_items,
		       total_bytes, pending_bytes, uploading_bytes, success_bytes, failed_bytes, skipped_bytes,
		       last_error, created_at, updated_at, started_at, completed_at
		FROM tasks
		ORDER BY created_at DESC`)
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
	row := r.db.QueryRow(`
		SELECT id, source, bucket, prefix_value, mode, status,
		       total_items, pending_items, uploading_items, success_items, failed_items, skipped_items,
		       total_bytes, pending_bytes, uploading_bytes, success_bytes, failed_bytes, skipped_bytes,
		       last_error, created_at, updated_at, started_at, completed_at
		FROM tasks WHERE id = ?`, id)
	item, err := scanTask(row.Scan)
	if err != nil {
		return task.Task{}, fmt.Errorf("scan task by id: %w", err)
	}

	return item, nil
}

func (r *SQLiteTaskRepository) ListItems(taskID string) ([]task.Item, error) {
	rows, err := r.db.Query(`
		SELECT task_id, path, relative_path, size, status, error_message, attempt_count,
		       created_at, updated_at, started_at, completed_at
		FROM task_items WHERE task_id = ? ORDER BY id ASC`, taskID)
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

func (r *SQLiteTaskRepository) UpdateTask(t task.Task) error {
	_, err := r.db.Exec(`
		UPDATE tasks SET
			status = ?,
			total_items = ?,
			pending_items = ?,
			uploading_items = ?,
			success_items = ?,
			failed_items = ?,
			skipped_items = ?,
			total_bytes = ?,
			pending_bytes = ?,
			uploading_bytes = ?,
			success_bytes = ?,
			failed_bytes = ?,
			skipped_bytes = ?,
			last_error = ?,
			updated_at = ?,
			started_at = ?,
			completed_at = ?
		WHERE id = ?`,
		string(t.Status),
		t.TotalItems,
		t.PendingItems,
		t.UploadingItems,
		t.SuccessItems,
		t.FailedItems,
		t.SkippedItems,
		t.TotalBytes,
		t.PendingBytes,
		t.UploadingBytes,
		t.SuccessBytes,
		t.FailedBytes,
		t.SkippedBytes,
		t.LastError,
		t.UpdatedAt.Format(time.RFC3339Nano),
		formatNullableTime(t.StartedAt),
		formatNullableTime(t.CompletedAt),
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("update task row: %w", err)
	}
	return nil
}

func (r *SQLiteTaskRepository) UpdateItemStatus(taskID string, relativePath string, status task.ItemStatus, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	completedAt := any(nil)
	if status == task.ItemStatusSuccess || status == task.ItemStatusFailed || status == task.ItemStatusSkipped {
		completedAt = now
	}

	startedExpr := "started_at"
	attemptIncrement := "attempt_count"
	if status == task.ItemStatusUploading {
		startedExpr = "COALESCE(started_at, ?)"
		attemptIncrement = "attempt_count + 1"
	}

	query := fmt.Sprintf(`
		UPDATE task_items
		SET status = ?,
		    error_message = ?,
		    attempt_count = %s,
		    updated_at = ?,
		    started_at = %s,
		    completed_at = ?
		WHERE task_id = ? AND relative_path = ?`, attemptIncrement, startedExpr)

	args := []any{string(status), errMsg, now}
	if status == task.ItemStatusUploading {
		args = append(args, now)
	}
	args = append(args, completedAt, taskID, relativePath)

	_, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("update task item status row: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) ResetItemsForRetry(taskID string) error {
	_, err := r.db.Exec(
		`UPDATE task_items
		 SET status = ?, error_message = '', attempt_count = 0, updated_at = ?, started_at = NULL, completed_at = NULL
		 WHERE task_id = ? AND status = ?`,
		string(task.ItemStatusPending),
		time.Now().UTC().Format(time.RFC3339Nano),
		taskID,
		string(task.ItemStatusFailed),
	)
	if err != nil {
		return fmt.Errorf("reset task item status rows: %w", err)
	}

	return nil
}

func (r *SQLiteTaskRepository) ClaimNextQueued() (task.Task, bool, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return task.Task{}, false, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(`
		SELECT id, source, bucket, prefix_value, mode, status,
		       total_items, pending_items, uploading_items, success_items, failed_items, skipped_items,
		       total_bytes, pending_bytes, uploading_bytes, success_bytes, failed_bytes, skipped_bytes,
		       last_error, created_at, updated_at, started_at, completed_at
		FROM tasks
		WHERE status = ?
		ORDER BY created_at ASC
		LIMIT 1`, string(task.StatusQueued))

	claimedTask, err := scanTask(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return task.Task{}, false, nil
		}
		return task.Task{}, false, fmt.Errorf("scan queued task: %w", err)
	}

	now := time.Now().UTC()
	claimedTask.Status = task.StatusRunning
	claimedTask.LastError = ""
	claimedTask.UpdatedAt = now
	if claimedTask.StartedAt == nil {
		claimedTask.StartedAt = &now
	}
	claimedTask.CompletedAt = nil

	_, err = tx.Exec(`
		UPDATE tasks
		SET status = ?, last_error = '', updated_at = ?, started_at = COALESCE(started_at, ?), completed_at = NULL
		WHERE id = ? AND status = ?`,
		string(task.StatusRunning),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		claimedTask.ID,
		string(task.StatusQueued),
	)
	if err != nil {
		return task.Task{}, false, fmt.Errorf("mark queued task running: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return task.Task{}, false, fmt.Errorf("commit claim transaction: %w", err)
	}

	return claimedTask, true, nil
}

type scanFn func(dest ...any) error

func scanTask(scan scanFn) (task.Task, error) {
	var item task.Task
	var status string
	var createdAt string
	var updatedAt string
	var startedAt sql.NullString
	var completedAt sql.NullString
	if err := scan(
		&item.ID,
		&item.Source,
		&item.Bucket,
		&item.Prefix,
		&item.Mode,
		&status,
		&item.TotalItems,
		&item.PendingItems,
		&item.UploadingItems,
		&item.SuccessItems,
		&item.FailedItems,
		&item.SkippedItems,
		&item.TotalBytes,
		&item.PendingBytes,
		&item.UploadingBytes,
		&item.SuccessBytes,
		&item.FailedBytes,
		&item.SkippedBytes,
		&item.LastError,
		&createdAt,
		&updatedAt,
		&startedAt,
		&completedAt,
	); err != nil {
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
	item.StartedAt, err = parseNullableTime(startedAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse started_at: %w", err)
	}
	item.CompletedAt, err = parseNullableTime(completedAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse completed_at: %w", err)
	}
	return item, nil
}

func scanTaskItem(scan scanFn) (task.Item, error) {
	var item task.Item
	var status string
	var createdAt string
	var updatedAt string
	var startedAt sql.NullString
	var completedAt sql.NullString
	if err := scan(
		&item.TaskID,
		&item.Path,
		&item.RelativePath,
		&item.Size,
		&status,
		&item.Error,
		&item.AttemptCount,
		&createdAt,
		&updatedAt,
		&startedAt,
		&completedAt,
	); err != nil {
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
	item.StartedAt, err = parseNullableTime(startedAt)
	if err != nil {
		return task.Item{}, fmt.Errorf("parse task_item started_at: %w", err)
	}
	item.CompletedAt, err = parseNullableTime(completedAt)
	if err != nil {
		return task.Item{}, fmt.Errorf("parse task_item completed_at: %w", err)
	}
	return item, nil
}

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
