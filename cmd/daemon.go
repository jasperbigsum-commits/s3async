package cmd

import (
	"fmt"
	"time"

	internaldaemon "github.com/jasperbigsum-commits/s3async/internal/daemon"
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

			status := internaldaemon.Status{
				PID:          pid,
				State:        "running",
				StartedAt:    time.Now().UTC(),
				HeartbeatAt:  time.Now().UTC(),
				PollInterval: pollInterval.String(),
				IdleTimeout:  idleTimeout.String(),
				AuditLogPath: manager.AuditLogPath(),
			}
			if err := manager.WriteStatus(status); err != nil {
				return fmt.Errorf("write daemon status: %w", err)
			}

			if err := runWorkerLoopFn(cmd, configPath, false, pollInterval, idleTimeout); err != nil {
				status.State = "error"
				status.HeartbeatAt = time.Now().UTC()
				status.LastError = err.Error()
				_ = manager.WriteStatus(status)
				return err
			}

			status.State = "stopped"
			status.HeartbeatAt = time.Now().UTC()
			now := time.Now().UTC()
			status.StoppedAt = now
			if err := manager.WriteStatus(status); err != nil {
				return fmt.Errorf("write daemon stop status: %w", err)
			}
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
