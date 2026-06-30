# cctv2 Test Plan

## Constraint

Real cameras are reachable only from `cctv2`.

Local development should therefore support a fake/simulated mode, while real stream validation happens on `cctv2`.

## Safety Rule

Do not disrupt the current CCTV service.

Early tests should:

- use a separate directory
- use a separate port
- manage only 1 or 2 test cameras
- avoid replacing current services
- avoid editing production go2rtc config or production DB

## Suggested cctv2 Layout

Initial test path:

```text
/opt/camstation2-test/
```

Initial ports:

```text
web console: 18080
internal go2rtc: separate local ports chosen by camstationd
```

## Test Phases

### Phase 1: Smoke Test

- start `camstationd`
- open web console
- confirm DB migrations
- confirm logs view
- confirm settings save/load

### Phase 2: One Camera

- add one camera
- test connection
- start live stream
- show connection state
- collect go2rtc status safely
- verify no raw credentials in UI/API/logs

### Phase 3: Recording

- start ffmpeg recorder worker
- create segments
- write DB metadata
- show recording timeline
- stop/restart worker
- confirm segment finalization

### Phase 4: Failure Behavior

- simulate camera disconnect or block stream
- observe state changes:
  - `streaming`
  - `degraded`
  - `reconnecting`
  - `offline`
- verify incident opens after sustained failure
- restore camera
- verify recovery and incident resolution

### Phase 5: Backup And Alerts

- test backup queue with safe test target
- test alert webhook with test endpoint
- verify cooldown, acknowledge, and resolve behavior

## Local Fake Mode

Local code should support a mode where no real camera is required:

- fake camera records state transitions
- fake go2rtc process adapter
- fake ffmpeg recorder adapter
- synthetic logs
- synthetic incidents

This keeps most development possible before deploying to `cctv2`.

