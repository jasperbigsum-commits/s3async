package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	internaldaemon "github.com/jasperbigsum-commits/s3async/internal/daemon"
	"github.com/jasperbigsum-commits/s3async/internal/filter"
	"github.com/jasperbigsum-commits/s3async/internal/scanner"
	"github.com/jasperbigsum-commits/s3async/internal/task"
	"github.com/jasperbigsum-commits/s3async/internal/uploader"
	"github.com/spf13/cobra"
)

var (
	newBootstrapWithConfig = app.NewBootstrapWithConfig
	runTaskOnceFn          = runTaskOnce
	runWorkerLoopFn        = runWorkerLoop
	spawnDaemonFn          = spawnDaemon
)

func newSyncCmd() *cobra.Command {
	var bucket string
	var prefix string
	var async bool
	var download bool
	var incremental bool
	var configPath string
	var include []string
	var exclude []string

	cmd := &cobra.Command{
		Use:   "sync <local-path>",
		Short: "Sync local files to S3, or download S3 files with --download",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := newBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}
			defer bootstrap.Close()

			source := args[0]
			resolvedBucket := bucket
			if resolvedBucket == "" {
				resolvedBucket = bootstrap.Config.Bucket
			}
			resolvedPrefix := prefix
			if !cmd.Flags().Changed("prefix") {
				resolvedPrefix = bootstrap.Config.Prefix
			}
			resolvedInclude := include
			if len(resolvedInclude) == 0 {
				resolvedInclude = bootstrap.Config.Filters.Include
			}
			resolvedExclude := exclude
			if len(resolvedExclude) == 0 {
				resolvedExclude = bootstrap.Config.Filters.Exclude
			}
			if resolvedBucket == "" {
				return fmt.Errorf("bucket is required via --bucket, config file, or S3ASYNC_BUCKET")
			}

			var items []task.Item
			service := bootstrap.TaskService
			var createdTask task.Task
			if download {
				source, err = filepath.Abs(source)
				if err != nil {
					return fmt.Errorf("resolve destination: %w", err)
				}
				// Listing is read-only and required even for dry-run planning.
				cfg := bootstrap.Config
				cfg.Bucket, cfg.S3.Bucket = resolvedBucket, resolvedBucket
				cfg.Security.DryRun = false
				client, clientErr := uploader.New(cmd.Context(), cfg)
				if clientErr != nil {
					return fmt.Errorf("create S3 client: %w", clientErr)
				}
				if incremental {
					items, err = client.PlanIncrementalDownload(cmd.Context(), resolvedBucket, resolvedPrefix, source, resolvedInclude, resolvedExclude)
				} else {
					items, err = client.PlanDownload(cmd.Context(), resolvedBucket, resolvedPrefix, source, resolvedInclude, resolvedExclude)
				}
				if err != nil {
					return fmt.Errorf("plan download: %w", err)
				}
				createdTask, err = service.CreateDownloadTask(source, resolvedBucket, resolvedPrefix, async, items)
			} else {
				entries, scanErr := scanner.Scan(source)
				if scanErr != nil {
					return fmt.Errorf("scan source: %w", scanErr)
				}
				for _, entry := range entries {
					if !filter.Match(entry.RelativePath, resolvedInclude, resolvedExclude) {
						continue
					}
					items = append(items, task.Item{Path: entry.Path, RelativePath: entry.RelativePath, Size: entry.Size, ModTime: entry.ModTime, Status: task.ItemStatusPending})
				}
				if incremental {
					cfg := bootstrap.Config
					cfg.Bucket, cfg.S3.Bucket = resolvedBucket, resolvedBucket
					cfg.Security.DryRun = false
					client, clientErr := uploader.New(cmd.Context(), cfg)
					if clientErr != nil {
						return fmt.Errorf("create S3 client: %w", clientErr)
					}
					items, err = client.PlanIncrementalUpload(cmd.Context(), resolvedBucket, resolvedPrefix, items)
					if err != nil {
						return fmt.Errorf("plan incremental upload: %w", err)
					}
				}
				createdTask, err = service.CreateTask(source, resolvedBucket, resolvedPrefix, async, items)
			}

			if err != nil {
				return fmt.Errorf("create task: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "task created: %s\n", createdTask.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", createdTask.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "planned items: %d\n", len(items))
			fmt.Fprintf(cmd.OutOrStdout(), "planned bytes: %d\n", createdTask.TotalBytes)

			if len(items) == 0 {
				if err := service.CompleteTaskIfEmpty(createdTask.ID); err != nil {
					return fmt.Errorf("complete empty task: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "task completed immediately: no matching files")
				return nil
			}

			if async {
				if err := spawnDaemonFn(configPath); err != nil {
					return fmt.Errorf("start daemon: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "daemon ensured")
				return nil
			}

			if err := runTaskOnceFn(createdTask.ID, configPath); err != nil {
				return fmt.Errorf("run task: %w", err)
			}

			if download {
				finished, err := service.GetTask(createdTask.ID)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "foreground download finished: %s\n", finished.Status)
				if finished.Status == task.StatusFailed || finished.Status == task.StatusPartialFailed {
					return fmt.Errorf("download failed: %s", finished.LastError)
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "foreground upload completed")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&download, "download", false, "Download S3 prefix into the local directory")
	cmd.Flags().BoolVar(&incremental, "incremental", false, "Skip unchanged files during upload or download")
	cmd.Flags().StringVar(&bucket, "bucket", "", "S3 bucket")
	cmd.Flags().StringVar(&prefix, "prefix", "", "S3 directory prefix")
	cmd.Flags().BoolVar(&async, "async", true, "Submit task in async mode")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	cmd.Flags().StringSliceVar(&include, "include", nil, "Include glob patterns")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "Exclude glob patterns")

	return cmd
}

func runTaskOnce(taskID string, configPath string) error {
	bootstrap, uploaderClient, execCfg, err := buildExecutionDeps(configPath)
	if err != nil {
		return err
	}
	defer bootstrap.Close()

	return bootstrap.TaskService.ExecuteTask(taskID, uploaderClient, execCfg)
}

func runWorkerLoop(cmd *cobra.Command, configPath string, once bool, pollInterval time.Duration, idleTimeout time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	bootstrap, uploaderClient, execCfg, err := buildExecutionDeps(configPath)
	if err != nil {
		return err
	}
	defer bootstrap.Close()

	startedAt := time.Now()
	lastWorkAt := startedAt
	for {
		executedTask, ok, err := bootstrap.TaskService.ExecuteNextQueuedTask(uploaderClient, execCfg)
		if err != nil {
			return fmt.Errorf("execute next queued task: %w", err)
		}
		if ok {
			fmt.Fprintf(cmd.OutOrStdout(), "worker executed task: %s (%s)\n", executedTask.ID, executedTask.Status)
			lastWorkAt = time.Now()
			if once {
				return nil
			}
			continue
		}

		if once {
			fmt.Fprintln(cmd.OutOrStdout(), "worker found no queued tasks")
			return nil
		}
		if idleTimeout > 0 && time.Since(lastWorkAt) >= idleTimeout {
			fmt.Fprintf(cmd.OutOrStdout(), "worker idle timeout reached after %s\n", time.Since(startedAt).Round(time.Second))
			return nil
		}
		time.Sleep(pollInterval)
	}
}

func buildExecutionDeps(configPath string) (*app.Bootstrap, *uploader.Client, task.ExecutionConfig, error) {
	bootstrap, err := newBootstrapWithConfig(configPath)
	if err != nil {
		return nil, nil, task.ExecutionConfig{}, fmt.Errorf("create bootstrap: %w", err)
	}

	uploaderClient, err := uploader.New(context.Background(), bootstrap.Config)
	if err != nil {
		_ = bootstrap.Close()
		return nil, nil, task.ExecutionConfig{}, fmt.Errorf("create uploader client: %w", err)
	}

	execCfg := task.ExecutionConfig{
		Workers:     bootstrap.Config.Workers,
		MaxAttempts: bootstrap.Config.Retry.MaxAttempts,
		Backoff:     time.Duration(bootstrap.Config.Retry.BackoffMS) * time.Millisecond,
	}
	return bootstrap, uploaderClient, execCfg, nil
}

func spawnDaemon(configPath string) error {
	bootstrap, err := newBootstrapWithConfig(configPath)
	if err != nil {
		return fmt.Errorf("create bootstrap: %w", err)
	}
	defer bootstrap.Close()

	manager := internaldaemon.NewManager(bootstrap.Config.StateDir)
	running, pid, err := manager.IsRunning()
	if err != nil {
		return fmt.Errorf("check daemon status: %w", err)
	}
	if running {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	args := []string{"daemon", "run"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	proc := exec.Command(exePath, args...)
	proc.Stdout = nil
	proc.Stderr = nil
	proc.Stdin = nil

	configSysProcAttr(proc)

	if err := proc.Start(); err != nil {
		if running && pid > 0 {
			return nil
		}
		return fmt.Errorf("start daemon process: %w", err)
	}

	return proc.Process.Release()
}

func executeCommand(root *cobra.Command, args ...string) (string, string, error) {
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func isAlreadyRunning(err error) bool {
	return errors.Is(err, internaldaemon.ErrAlreadyRunning)
}
