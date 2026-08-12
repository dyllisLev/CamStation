# Pinned Windows control setup and audit

Use setup only on the explicitly selected authorized Windows PC when `Status` shows the pinned
driver is missing or unhealthy, or when the user asks to install/audit it. Existing exact
installations are audited and repaired idempotently; mismatched existing directories fail closed.

Canonical implementation:

```text
scripts/windows/Install-CamStationWindowsControl.ps1
```

The script pins driver version 0.19.3, the official release archive SHA-256, the exact six-file set,
and each installed file SHA-256. Treat those constants as a reviewed supply-chain boundary; update
them only in an explicit driver-upgrade change with a fresh official-source verification and a real
host test.

## Fresh install

1. Download the exact Windows x86_64 archive from the official Cua release source into a unique
   temporary path outside the repository. Do not use a mirror or an unpinned mutable URL.
2. Calculate the archive SHA-256 locally and on Windows. Do not invoke setup unless it matches the
   pinned value in the installer.
3. Hash-check the local and remote installer copies, then invoke through the target wrapper:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target <alias> --mode setup --archive <pinned-cua-driver.zip>
```

The installer extracts to a unique staging directory, rejects unexpected files or hashes, and only
then moves the complete version directory into `C:\Program Files\Cua Driver`. It never overwrites a
partial or mismatched existing version.

Setup uses one temporary interactive scheduled task to run `telemetry disable`, `autostart enable`,
and `autostart kick` for the intended desktop user. It removes the exact temporary task and staging
directory in `finally`. It does not create a password, listener, firewall rule, new account, or
remote desktop service.

## Existing install audit

When the exact version directory already exists, omit `--archive`; the same path verifies all six
installed hashes and runs the bounded telemetry/autostart repair:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode setup
```

Require `WINDOWS_CONTROL_SETUP_COMPLETE`, six `Matches=true` file records,
`TelemetryDisabled=true`, exactly one driver daemon in the intended nonzero session, the vendor
`cua-driver-serve` task belonging to the intended interactive user, and
`TemporarySetupTaskCount=0`. Report each Authenticode status; an official archive/hash match does
not make `NotSigned` equivalent to signed software.

Then run the unified launcher's `Status` mode and require zero driver TCP connections, zero Cua
firewall rules, zero control tasks, and the expected Viewer service state.

## Failure and rollback

On hash, identity, session, task-result, telemetry, or daemon-count failure, stop and preserve the
failing summary. Never replace a mismatched install automatically. Remove only a known staging
directory or exact temporary task created by the current run.

When the user explicitly requests full rollback, disable the target user's vendor autostart, stop
only the matching daemon in that session, and remove the exact pinned version directory and that
user's Cua configuration. Resolve and report every exact target first. No network rollback should
be required because normal setup creates no listener or firewall rule.
