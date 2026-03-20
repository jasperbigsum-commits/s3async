package cmd

import (
	"fmt"
	"time"

	internaldaemon "github.com/jasperbigsum-commits/s3async/internal/daemon"
	internallogging "github.com/jasperbigsum-commits/s3async/internal/logging"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Manage background daemon"}
	cmd.AddCommand(newDaemonRunCmd())
	cmd.AddCommand(newDaemonStatusCmd())
	cmd.AddCommand(newDaemonStopCmd())
	return cmd
}

func newDaemonRunCmd() *cobra.Command {
	var configPath string
	var pollInterval time.Duration
	var idleTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the queue daemon loop",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := newBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}

			manager := internaldaemon.NewManager(bootstrap.Config.StateDir)
			pid, release, err := manager.Acquire()
			if err != nil {
				return fmt.Errorf("acquire daemon pid file: %w", err)
			}
			defer release()
			defer manager.ClearStopRequest()

			auditRecorder, err := internallogging.NewFileAuditRecorder(manager.AuditLogPath())
			if err != nil {
				return fmt.Errorf("open audit log: %w", err)
			}
			defer auditRecorder.Close()

			bootstrap, uploaderClient, execCfg, err := buildExecutionDeps(configPath)
			if err != nil {
				return err
			}

			startedAt := time.Now().UTC()
			status := internaldaemon.Status{
				PID:          pid,
				State:        "running",
				StartedAt:    startedAt,
				HeartbeatAt:  startedAt,
				PollInterval: pollInterval.String(),
				IdleTimeout:  idleTimeout.String(),
				AuditLogPath: manager.AuditLogPath(),
			}
			if err := manager.WriteStatus(status); err != nil {
				return fmt.Errorf("write daemon status: %w", err)
			}
			_ = auditRecorder.Record(internallogging.AuditEvent{Event: "daemon_started", DaemonPID: pid, DaemonState: status.State, Message: "daemon loop started"})

			lastWorkAt := startedAt
			for {
				now := time.Now().UTC()
				status.HeartbeatAt = now
				status.QueuePolls++
				if manager.StopRequested() {
					status.State = "stopping"
					status.StoppedAt = now
					_ = manager.WriteStatus(status)
					_ = auditRecorder.Record(internallogging.AuditEvent{Event: "daemon_stop_requested", DaemonPID: pid, DaemonState: status.State, Message: "stop file detected"})
					break
				}

				executedTask, ok, err := bootstrap.TaskService.ExecuteNextQueuedTask(uploaderClient, execCfg)
				if err != nil {
					status.State = "error"
					status.LastError = err.Error()
					status.HeartbeatAt = time.Now().UTC()
					_ = manager.WriteStatus(status)
					_ = auditRecorder.Record(internallogging.AuditEvent{Event: "daemon_error", DaemonPID: pid, DaemonState: status.State, Error: err.Error(), Message: "execute next queued task failed"})
					return fmt.Errorf("execute next queued task: %w", err)
				}
				if ok {
					status.TasksExecuted++
					status.CurrentTaskID = executedTask.ID
					status.LastTaskID = executedTask.ID
					status.LastTaskStatus = string(executedTask.Status)
					status.LastError = executedTask.LastError
					status.HeartbeatAt = time.Now().UTC()
					_ = manager.WriteStatus(status)
					_ = auditRecorder.Record(internallogging.AuditEvent{Event: "task_executed", DaemonPID: pid, DaemonState: status.State, TaskID: executedTask.ID, TaskStatus: string(executedTask.Status), Error: executedTask.LastError, Bucket: executedTask.Bucket, Prefix: executedTask.Prefix, Source: executedTask.Source, Message: "queued task executed"})
					fmt.Fprintf(cmd.OutOrStdout(), "daemon executed task: %s (%s)\n", executedTask.ID, executedTask.Status)
					status.CurrentTaskID = ""
					lastWorkAt = time.Now().UTC()
					continue
				}

				status.CurrentTaskID = ""
				status.HeartbeatAt = time.Now().UTC()
				_ = manager.WriteStatus(status)
				if idleTimeout > 0 && time.Since(lastWorkAt) >= idleTimeout {
					status.State = "idle_timeout"
					status.StoppedAt = time.Now().UTC()
					_ = manager.WriteStatus(status)
					_ = auditRecorder.Record(internallogging.AuditEvent{Event: "daemon_idle_timeout", DaemonPID: pid, DaemonState: status.State, Message: "idle timeout reached"})
					break
				}
				time.Sleep(pollInterval)
			}

			status.State = "stopped"
			status.HeartbeatAt = time.Now().UTC()
			now := time.Now().UTC()
			status.StoppedAt = now
			if err := manager.WriteStatus(status); err != nil {
				return fmt.Errorf("write daemon stop status: %w", err)
			}
			_ = auditRecorder.Record(internallogging.AuditEvent{Event: "daemon_stopped", DaemonPID: pid, DaemonState: status.State, Message: "daemon loop stopped"})
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "How often the daemon polls for queued tasks")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 0, "Exit after this much idle time without queued tasks (0 = never)")
	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := newBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}
			manager := internaldaemon.NewManager(bootstrap.Config.StateDir)

			running, pid, err := manager.IsRunning()
			if err != nil {
				return fmt.Errorf("check daemon status: %w", err)
			}
			status, err := manager.ReadStatus()
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "running: %t\n", running)
				fmt.Fprintf(cmd.OutOrStdout(), "pid: %d\n", pid)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "running: %t\n", running)
			fmt.Fprintf(cmd.OutOrStdout(), "pid: %d\n", status.PID)
			fmt.Fprintf(cmd.OutOrStdout(), "state: %s\n", status.State)
			fmt.Fprintf(cmd.OutOrStdout(), "started_at: %s\n", formatTimestamp(status.StartedAt))
			fmt.Fprintf(cmd.OutOrStdout(), "heartbeat_at: %s\n", formatTimestamp(status.HeartbeatAt))
			fmt.Fprintf(cmd.OutOrStdout(), "stopped_at: %s\n", formatTimestamp(status.StoppedAt))
			if status.LastError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "last_error: %s\n", status.LastError)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "state_dir: %s\n", status.StateDir)
			fmt.Fprintf(cmd.OutOrStdout(), "audit_log: %s\n", status.AuditLogPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Request daemon stop",
		RunE: func(cmd *cobra.Command, args []string) error {
			bootstrap, err := newBootstrapWithConfig(configPath)
			if err != nil {
				return fmt.Errorf("create bootstrap: %w", err)
			}
			manager := internaldaemon.NewManager(bootstrap.Config.StateDir)
			if err := manager.RequestStop(); err != nil {
				return fmt.Errorf("request daemon stop: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "daemon stop requested")
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")
	return cmd
}
