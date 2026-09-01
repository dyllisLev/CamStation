# Forgejo + OpenShip production deployment design

> Status: discovery and rollout in progress
>
> Change boundary: no production replacement before backup and rollback gates pass

## Outcome

CamStation production releases originate from the Forgejo default branch, are built once as an
immutable image by a self-hosted Forgejo Actions runner, are pushed to the Forgejo Container
Registry, and are deployed explicitly through the external OpenShip API. GitHub remains a
one-way Push Mirror destination and has no production build or deployment authority.

```text
developer push
  -> Forgejo default branch
  -> Forgejo Actions (self-hosted, shared build lock)
  -> git.loc.hmini.me/dyllislev/camstation:sha-<commit>
  -> OpenShip service image PATCH
  -> OpenShip deployment
  -> existing LOC production route
```

## Repository and release contract

- Primary repository: `https://git.loc.hmini.me/dyllislev/CamStation`
- Default branch: `main`
- Mirror direction: Forgejo to the existing GitHub repository only
- LFS and submodules: preserve all required objects; absence must be explicitly verified
- Production trigger: push to the exact default branch only
- Image repository: `git.loc.hmini.me/dyllislev/camstation`
- Image tag: `sha-<full Forgejo commit SHA>`
- Build context: repository root
- Docker target: `runtime`
- Platform: `linux/amd64`, matching the selected `cctv-production` OpenShip server

The workflow never uses a mutable production tag for deployment. OCI labels identify the Forgejo
source repository and exact commit. Registry credentials exist only in a temporary Docker config
owned by the job and are passed to login through standard input.

## Application runtime contract

CamStation is one long-running daemon. It owns the embedded web console and supervises go2rtc,
recording ffmpeg workers, cleanup, backup scheduling, and SQLite migrations. It listens on HTTP
port `18080`; WebRTC uses container port `8555` over TCP and UDP through the existing LOC host
publishes. OpenShip 0.6.9 overwrites repeated host-IP bindings for one container port/protocol, so
the two verified physical LOC interfaces are represented by one IPv4 wildcard HTTP publish and one
IPv4 wildcard publish per WebRTC protocol. This normalization is allowed only while the deployment
host has no public or Tailscale/overlay IPv4 interface; a changed interface inventory is a stop
condition. The container health endpoint is `GET /api/health`.

The final image runs as the existing non-root CamStation UID/GID where persistent volume ownership
permits it. Runtime configuration and application secrets are injected by OpenShip, never baked
into the image. The SQLite database, Viewer releases, recordings, temporary recording files and
rclone state/config retain their current persistent locations and atomic-filesystem requirements.

Exact environment names, secret names, logical mount identities, published ports, domain and agent
are populated in `docs/deployment.md` only after safe discovery. Secret values, raw host paths and
camera/backup URLs are never recorded.

## OpenShip contract

- Environment: `production`
- Source repository/GitHub integration: absent
- Webhook and Auto Deploy: off
- Sleep mode: `always_on`
- Registry pull credential: `openship-registry-pull-20260901`
- Service `image`: exact Forgejo Registry image
- Service `build` and `dockerfile`: empty
- Restart policy, health check, ports, volumes, resources, network and dependencies: copied from the
  verified current production contract and changed only where OpenShip requires an equivalent form
- The service stores `advanced.stopGracePeriod: 120s`; recorder shutdown broadcasts stop to every
  worker before waiting, and only each worker's process-wait path signals FFmpeg. The daemon must
  finish inside OpenShip's 30-second previous-container teardown wait despite retaining the longer
  Docker safety ceiling.
- External API URL: discovered from the current operating domain and required to end in
  `/api/proxy/api`

Actions PATCHes every configured service ID to the new image while clearing Git build fields,
creates one deployment, polls every five seconds for at most fifteen minutes, and succeeds only on
`ready`. Every other OpenShip terminal state (`failed`, `cancelled`, `action_required`,
`partial_failure`, `rejected`, or `no_changes`) fails the workflow; diagnostics include bounded
error metadata, recent deployment logs, and pending action metadata without credentials.

## Backup and rollback contract

Before the first OpenShip deployment, capture:

1. current image ID/reference and source revision;
2. root-owned deployment configuration and non-secret environment-name manifest;
3. an SQLite online backup with integrity verification, including the required database state;
4. persistent-volume inventory and a safe backup of non-recording state plus the established media
   backup/retention mechanism;
5. current internal and external health evidence.

The existing production service stays available until these artifacts are verified. Rollback
restores the exact former image and deployment configuration while keeping the persistent data
mounts. A schema migration is accepted only when startup migration is forward-compatible with the
former application or a separately verified data restore is available. GitHub deployment is not
disabled until the Forgejo/OpenShip path passes all application gates.

## Acceptance gates

- Forgejo contains the default branch, required branches and tags at the expected SHAs.
- Push Mirror has an empty `last_error`, and GitHub receives a Forgejo-originated commit/ref update
  without starting production deployment.
- The registry exposes the exact `sha-<commit>` image for the target platform.
- OpenShip project/service configuration has no Git build source and uses the named pull credential.
- The Forgejo workflow completes and its OpenShip deployment reaches `ready`.
- The running container reports the exact image SHA, no restart loop, healthy internal endpoint and
  no fatal recent logs.
- External TLS/domain request succeeds through the current LOC route.
- Both pre-existing physical LOC endpoints remain reachable after the wildcard port normalization,
  and no public or Tailscale/overlay IPv4 interface has appeared on the host.
- SQLite opens successfully, persistent mounts are correct, recorder/cleanup/backup scheduler
  behavior is observed, and data survives a controlled redeploy.
- A controlled redeploy completes without a previous-container teardown timeout or fixed-port
  collision, and every segment closed by shutdown matches its database size and passes `ffprobe`.
- GitHub production deployment is absent or disabled, while non-deployment checks are either moved
  to Forgejo or intentionally retained without self-hosted production authority.

## Stop conditions

Stop before production mutation if the operating host, persistent-data target, external domain,
OpenShip agent, credential authority, or reversible backup cannot be determined from actual state.
Do not bypass SSH host verification, edit OpenShip-managed Compose or databases, edit Zoraxy's
database, force-push refs, or expose credential values to diagnose a failure.
