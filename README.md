# Agent Coffee

Launch Claude Code or Codex in the foreground on macOS. Agent Coffee uses turn hooks to hold an idle-sleep assertion while the agent works. The display follows normal sleep settings by default. Each user has one `acd claude` and one `acd codex`, started on demand and shared by that harness's terminals. Optional closed-lid operation uses one system-wide `lidd` service.

## Build and use

Build with Go 1.22 or later, cgo enabled, and Apple's command-line developer tools. Choose a stable binary path because Codex records the hook command.

```sh
mkdir -p ~/.local/bin
go build -o ~/.local/bin/agent-coffee .
~/.local/bin/agent-coffee config
~/.local/bin/agent-coffee codex
# or
~/.local/bin/agent-coffee claude
```

The config command opens `~/.agent-coffee/config/config.yml` in your editor. Valid settings include:

```yaml
defaults:
  lid_closed: false
  keep_display_on: false
  max_awake: "90m"
claude:
  max_awake: "60m"
```

When the new config is missing, the launcher and config editor copy an existing `~/.config/agent-coffee/config.yml` to the new location.

The launcher, config editor, and per-harness daemons append structured JSON events to `~/.agent-coffee/log/agent-coffee.log`. Watch turn holds and releases with:

```sh
tail -f ~/.agent-coffee/log/agent-coffee.log
```

The privileged lid helper writes its recovery diagnostics to `/var/log/agent-coffee-lidd.log` through launchd.

At first Codex launch, review the Agent Coffee commands in Codex's `/hooks` screen and trust them to enable turn tracking. Registration merges entries into `$CODEX_HOME/hooks.json` (normally `~/.codex/hooks.json`). Claude receives additional hook settings for the launched session. A timed `caffeinate` child holds idle sleep and optionally display sleep. A matched stop/failure/interrupt, launcher exit, or the deadline releases it. Approval waits currently count toward the turn's deadline. Hook delivery on cancellation and continuation depends on the provider version. Keep Terminal open until the task finishes.

## Optional closed-lid operation

Install the system helper for the current user before setting `lid_closed: true`. The installer copies the built executable to a root-owned path and registers one launchd service. Each active turn requests its own bounded lease. The helper verifies the effective `SleepDisabled` state through IOKit registry readback, journals changes, and restores the prior setting after the last lease ends. On restart it retries pending recovery. Readback or acquisition failures are reported. The actual closed-lid behavior on battery still requires a physical test on the target Mac.

```sh
sudo scripts/install-lidd "$HOME/.local/bin/agent-coffee"
# To remove the helper after active lid holds finish
sudo scripts/uninstall-lidd
```

## Repository layout

The root `main.go` dispatches commands for the single executable. Under `internal/`, `harness/` launches Claude and Codex, `acd/` owns turn-level sleep holds, and `lidd/` manages the machine-wide lid setting. Configuration, socket messaging, file operations, and the sleep probe each have their own package. Tests live alongside the code they exercise.

The [architecture](docs/ARCHITECTURE.md) and [low-level design](docs/LLD.md) describe the system. The `scripts/` directory contains the lid helper installer and uninstaller.

## macOS sleep experiment

The Go probe writes a timestamped heartbeat to a temporary log every two seconds. It records the power source, sleep settings, and active assertions. `baseline` observes normal behavior. `idle` holds a time-limited `caffeinate -i` assertion for the run. Both modes use your current macOS power settings.

Build with Go 1.22 or later.

```sh
go build -o /tmp/agent-coffee .
```

The probe prints the log path when it starts. Wait for it to finish or press Ctrl-C to stop. Keep the Mac on a ventilated desk for all four runs.

| Run | Command | What to do |
| --- | --- | --- |
| Lid open, baseline | `/tmp/agent-coffee -mode baseline -duration 15m` | Walk away. Choose a duration longer than the sleep and display idle timers for your power source. |
| Lid open, idle assertion | `/tmp/agent-coffee -mode idle -duration 15m` | Repeat with the same power source, peripherals, and duration. |
| Lid closed, baseline | `/tmp/agent-coffee -mode baseline -duration 2m` | Unplug power and any external display. Start the probe, wait five seconds, close the lid for 30 seconds, then reopen it. |
| Lid closed, idle assertion | `/tmp/agent-coffee -mode idle -duration 2m` | Repeat the same sequence on battery. Waiting five seconds gives `caffeinate` time to register and the probe time to capture its assertion. |

After each run, compare the heartbeat timestamps with the sleep and wake history.

```sh
pmset -g log | tail -n 120
```

- A heartbeat gap around the lid-closed period plus a matching sleep and wake shows the probe stopped running while the Mac slept.
- Heartbeats about two seconds apart throughout the interval show that the probe kept running in that setup. Screen dimming or going black alone is inconclusive.
- If the idle baseline stays awake, check the recorded `pmset -g assertions` output for background apps holding assertions. The idle comparison is inconclusive until the baseline can sleep.
- In `idle` mode, look for `caffeinate` holding `PreventUserIdleSystemSleep` in the second assertion snapshot. Lid-close sleep can still occur with that assertion held.
