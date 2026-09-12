package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"media-server-pro/internal/config"
)

// healthPingTimeout bounds a single liveness ping. Short on purpose: the point
// of the ping is to answer "is the pool usable right now", and a ping that
// needs more than a few seconds has already answered "no" for any practical
// purpose.
const healthPingTimeout = 3 * time.Second

// recoveryCommandName is the script looked for next to the server binary when
// no explicit RecoveryCommand is configured. The systemd unit runs the binary
// as __DEPLOY_DIR__/server with WorkingDirectory=__DEPLOY_DIR__, and deploy.sh
// lives in that same directory.
const recoveryCommandName = "deploy.sh"

// startHeartbeat launches the background liveness heartbeat.
//
// Health() only pings when something asks it to, which means a connection lost
// while the server is idle is not noticed until the next request — and the
// request is what pays for the discovery. The heartbeat moves that cost off the
// request path and, more importantly, gives a sustained outage somewhere to be
// observed and acted on.
func (m *Module) startHeartbeat(dbCfg config.DatabaseConfig) {
	if !dbCfg.HeartbeatEnabled {
		m.log.Info("Database heartbeat disabled")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.bgMu.Lock()
	m.bgCancel = cancel
	m.bgMu.Unlock()

	m.log.Info("Database heartbeat: every %v, recovery after %d consecutive failures (recovery %s)",
		dbCfg.HeartbeatInterval, dbCfg.HeartbeatThreshold, enabledWord(dbCfg.RecoveryEnabled))

	m.bgWG.Go(func() { m.heartbeatLoop(ctx, dbCfg) })
}

// stopHeartbeat cancels the heartbeat and waits for it to exit.
//
// Joining matters: the loop pings through m.sqlDB, so letting it outlive Stop
// would leave it using a pool that Stop is closing.
func (m *Module) stopHeartbeat() {
	m.bgMu.Lock()
	cancel := m.bgCancel
	m.bgCancel = nil
	m.bgMu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.bgWG.Wait()
}

// heartbeatLoop pings on an interval, tracking consecutive failures and firing
// the recovery action when they cross the configured threshold.
//
// The failure counter and attempt bookkeeping are loop-local: this runs as a
// single goroutine, so they need no locking, and keeping them out of the module
// means a Stop/Start cycle starts from a clean slate rather than inheriting the
// failure count that caused the restart.
func (m *Module) heartbeatLoop(ctx context.Context, dbCfg config.DatabaseConfig) {
	ticker := time.NewTicker(dbCfg.HeartbeatInterval)
	defer ticker.Stop()

	consecutiveFailures := 0
	recoveryAttempts := 0
	var lastRecovery time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if err := m.heartbeatPing(ctx); err != nil {
			// A cancelled context is shutdown, not a database fault; counting it
			// would let a restart look like an outage.
			if ctx.Err() != nil {
				return
			}
			consecutiveFailures++
			m.setHealth(false, fmt.Sprintf("Heartbeat failed: %v", err))
			m.log.Warn("Database heartbeat failed (%d/%d consecutive): %v",
				consecutiveFailures, dbCfg.HeartbeatThreshold, err)

			if consecutiveFailures < dbCfg.HeartbeatThreshold {
				continue
			}
			// Reset the counter whether or not the attempt is allowed to run, so
			// a declined attempt (cooldown, attempts exhausted) re-arms after
			// another full threshold of failures instead of retrying every tick.
			consecutiveFailures = 0
			m.maybeRecover(dbCfg, &recoveryAttempts, &lastRecovery)
			continue
		}

		if consecutiveFailures > 0 {
			m.log.Info("Database heartbeat recovered after %d consecutive failures", consecutiveFailures)
			consecutiveFailures = 0
			// Clear the attempt budget too: the connection came back, so a later
			// unrelated outage gets a full set of attempts rather than inheriting
			// the spent ones.
			recoveryAttempts = 0
		}
		m.setHealth(true, "Connected")
	}
}

// heartbeatPing pings the pool, reporting a missing pool as a failure.
func (m *Module) heartbeatPing(ctx context.Context) error {
	m.dbMu.RLock()
	sqlDB := m.sqlDB
	m.dbMu.RUnlock()

	if sqlDB == nil {
		return fmt.Errorf("no database connection")
	}

	pingCtx, cancel := context.WithTimeout(ctx, healthPingTimeout)
	defer cancel()
	return sqlDB.PingContext(pingCtx)
}

// recoveryDecision is the outcome of the recovery guards.
type recoveryDecision int

const (
	recoveryRun         recoveryDecision = iota // guards passed; run the command
	recoveryDisabled                            // RecoveryEnabled is false
	recoveryExhausted                           // RecoveryMaxAttempts reached
	recoveryCoolingDown                         // inside RecoveryCooldown since the last attempt
)

// recoveryGate decides whether a recovery attempt may proceed. Split out from
// maybeRecover so the guards can be tested without executing anything: the
// action they protect restarts the server.
func recoveryGate(dbCfg config.DatabaseConfig, attempts int, lastRecovery, now time.Time) recoveryDecision {
	switch {
	case !dbCfg.RecoveryEnabled:
		return recoveryDisabled
	case dbCfg.RecoveryMaxAttempts > 0 && attempts >= dbCfg.RecoveryMaxAttempts:
		return recoveryExhausted
	// The cooldown is what keeps a database that is down for an hour from
	// triggering an hour of back-to-back deploys.
	case !lastRecovery.IsZero() && now.Sub(lastRecovery) < dbCfg.RecoveryCooldown:
		return recoveryCoolingDown
	default:
		return recoveryRun
	}
}

// maybeRecover runs the recovery command if the guards allow it. attempts and
// lastRecovery are owned by the heartbeat loop and updated here.
func (m *Module) maybeRecover(dbCfg config.DatabaseConfig, attempts *int, lastRecovery *time.Time) {
	now := time.Now()
	switch recoveryGate(dbCfg, *attempts, *lastRecovery, now) {
	case recoveryDisabled:
		m.log.Error("Database unreachable for %d consecutive heartbeats; recovery is disabled (set DATABASE_RECOVERY_ENABLED=true to auto-restart)",
			dbCfg.HeartbeatThreshold)
		return
	case recoveryExhausted:
		m.log.Error("Database still unreachable but recovery attempts are exhausted (%d/%d); manual intervention required",
			*attempts, dbCfg.RecoveryMaxAttempts)
		return
	case recoveryCoolingDown:
		m.log.Warn("Database still unreachable but last recovery was %v ago; waiting out the %v cooldown",
			now.Sub(*lastRecovery).Round(time.Second), dbCfg.RecoveryCooldown)
		return
	case recoveryRun:
	}

	cmdPath, err := resolveRecoveryCommand(dbCfg.RecoveryCommand)
	if err != nil {
		m.log.Error("Database recovery skipped: %v", err)
		return
	}

	*attempts++
	*lastRecovery = now
	m.log.Error("Database unreachable for %d consecutive heartbeats — running recovery (attempt %d): %s %v",
		dbCfg.HeartbeatThreshold, *attempts, cmdPath, dbCfg.RecoveryArgs)

	if err := launchRecovery(cmdPath, dbCfg.RecoveryArgs); err != nil {
		m.log.Error("Database recovery command failed to start: %v", err)
		return
	}
	m.log.Info("Database recovery command started; it is expected to restart this process")
}

// resolveRecoveryCommand returns the absolute path of the recovery script.
//
// An empty configured value falls back to deploy.sh beside the server binary,
// which is where deploy.sh puts itself. The result is checked for existence
// here rather than at exec time so a typo is reported as a clear config problem
// instead of an opaque "no such file" in the middle of an outage.
func resolveRecoveryCommand(configured string) (string, error) {
	path := configured
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("no recovery_command set and the executable path is unresolvable: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		path = filepath.Join(filepath.Dir(exe), recoveryCommandName)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("recovery command %q is not a resolvable path: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("recovery command %q is not usable: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("recovery command %q is a directory", abs)
	}
	return abs, nil
}

// launchRecovery starts the recovery command and returns without waiting.
//
// Not waiting is the whole point: the command's job is to restart this service,
// so it has to outlive the process that started it. Two things follow from that,
// and both are deliberate:
//
//   - No context and no timeout. Tying the command to a cancellable context
//     would have Stop() kill the deploy partway through — most likely just
//     after it ran `systemctl stop`, leaving nothing running and nothing left
//     to finish the job.
//   - Under systemd, the command is handed to systemd-run when available so it
//     lands in its own cgroup. A plain child stays inside this unit's cgroup,
//     where the deploy's own `systemctl stop` kills it along with the server.
//     See recoveryDetachAttrs for the non-systemd case.
func launchRecovery(cmdPath string, args []string) error {
	name, argv := cmdPath, args
	if runner, ok := systemdRunPrefix(); ok {
		name = runner[0]
		argv = append(append([]string{}, runner[1:]...), append([]string{cmdPath}, args...)...)
	}

	cmd := exec.Command(name, argv...) //nolint:gosec // G204: cmdPath is an operator-configured path validated by resolveRecoveryCommand; not request-derived
	cmd.Dir = filepath.Dir(cmdPath)
	// Inherit the environment so the script sees the same .env-derived settings
	// the server was started with.
	cmd.Env = os.Environ()
	// The parent is about to be restarted by this very command, so there is no
	// one left to read pipes. Sending output to the journal/stdout keeps the
	// deploy log somewhere an operator can find it.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	recoveryDetachAttrs(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the child so it does not linger as a zombie in the (possibly long)
	// window before this process is replaced. Release() is not enough on Unix:
	// the child must be waited on by its parent.
	go func() { _ = cmd.Wait() }()
	return nil
}

// systemdRunPrefix returns the systemd-run invocation to wrap the recovery
// command with, when running under systemd and systemd-run is available.
//
// A transient unit (--unit, not --scope) is started by PID 1, so it is not a
// descendant of this service and survives the `systemctl stop` that the deploy
// performs on itself. --collect discards the unit once it exits so repeated
// attempts do not accumulate failed units, and --no-block keeps this call from
// waiting on the deploy.
func systemdRunPrefix() ([]string, bool) {
	// INVOCATION_ID is set by systemd for its own services; absent it, we are
	// running from a shell or a container and the cgroup problem does not apply.
	if os.Getenv("INVOCATION_ID") == "" {
		return nil, false
	}
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return nil, false
	}
	unit := "msp-db-recovery-" + strconv.FormatInt(time.Now().Unix(), 10)
	return []string{
		path,
		"--collect",
		"--no-block",
		"--unit=" + unit,
		"--description=Media Server Pro database recovery",
	}, true
}

// enabledWord renders a bool for log lines.
func enabledWord(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}
