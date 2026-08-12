# Camera Enable/Disable Design

## Goal

Allow an operator to persistently enable or disable each registered camera from
the camera settings page. A disabled camera must remain editable in settings but
must not cause camera connection, playback, preload, recording, preview, PTZ, or
automatic retry activity. The state must survive daemon restarts.

## Selected Design

Add a dedicated persisted `enabled` field to the camera record. Do not reuse the
camera `state` field because `state` describes observed runtime condition while
`enabled` is operator intent.

Expose the field in the redacted public camera DTO and add a dedicated endpoint:

```text
PATCH /api/cameras/{streamName}/enabled
{"enabled": true|false}
```

The registered-camera table on `/cameras` shows an activation switch and a clear
`활성` or `비활성` label. Switching it disables further clicks while the server
applies the change. Disabled cameras stay visible and editable on this settings
page.

## Runtime Behavior

When a camera is disabled:

- its private inputs, public outputs, and preloads are omitted from generated
  go2rtc configuration;
- any recorder worker for the camera is stopped during reconciliation;
- live and operational UI surfaces do not offer it for playback;
- preview, PTZ, stream-control, and similar connection-producing operations are
  rejected without contacting the camera;
- daemon restart continues to exclude it from streaming and recording.

When a camera is enabled, the existing stream policy and recorder reconciliation
path applies its persisted sources and outputs. Camera-source recovery behavior
for enabled cameras remains unchanged; this feature only guarantees that a
disabled camera performs no connection or retry activity.

## Apply and Failure Handling

The server serializes activation changes with the existing camera-policy apply
path. It persists the requested value, regenerates the enabled-camera runtime
configuration, and reconciles recorder workers.

If runtime application fails, the server restores the prior persisted value and
keeps or reapplies the prior known-good runtime configuration. The API returns a
redacted operational error, and the UI restores the previous switch state with a
Korean failure message. No raw URL, credential, internal transport alias, or
generated command is exposed.

## Existing Data and This Server

The schema migration gives existing camera records an enabled default so generic
upgrades preserve prior behavior. During deployment to this server, activation
values are seeded while the daemon is stopped and before camera workers read the
new field.

The required initial state is:

- enabled: `집-마당`, `집-창고1`, `집-창고2`;
- disabled: `염소장`, `소방서1`, `소방서3`, `소방서4`, `소방서5`.

The daemon is then started with recording still disabled, and runtime evidence
must confirm that disabled cameras have no go2rtc stream, preload, producer, or
recorder worker.

## UI and API Compatibility

Camera administration receives all registered cameras so disabled entries can
be managed. Live/playback consumers use the explicit `enabled` field and must not
request disabled stream names. New camera registration defaults to enabled,
matching the existing registration workflow.

Older API consumers that ignore the additive field remain compatible. Existing
camera URLs and credentials remain confined to the store and generated private
configuration.

## Verification and Review Scope

Focused automated checks cover:

- migration, camera reads/writes, and persisted activation changes;
- public DTO redaction plus the activation endpoint;
- exclusion of disabled cameras from generated stream/preload configuration and
  recorder reconciliation;
- rejection of connection-producing operations for disabled cameras;
- settings switch behavior and live-surface filtering.

Run only the relevant Go tests plus frontend lint/build and the final daemon
build. Review only files changed for this task. After deployment, verify the API,
database state, go2rtc streams/preloads, and recorder status without opening a
live view that could create unrelated playback activity.

## Non-Goals

- Changing camera credentials, profiles, source URLs, or video encoding policy
- Reworking the existing enabled-camera reconnect strategy
- Deleting disabled cameras or their historical recordings
- Adding bulk scheduling, role-based permissions, or remote Internet controls
- Performing a whole-project review before all planned implementation tasks end
