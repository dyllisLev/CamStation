# Forgejo + OpenShip production deployment plan

**Goal:** Move CamStation's production image build and deployment authority from GitHub/manual
delivery to Forgejo Actions, the Forgejo Container Registry and explicit OpenShip deployments while
preserving the existing LOC route, SQLite data and a tested rollback path.

**Architecture:** One immutable multi-stage Docker image contains the CamStation daemon, embedded
web console, go2rtc, ffmpeg and operational dependencies. A default-branch-only Forgejo workflow
serializes shared builds, publishes the target-platform image, updates image-only OpenShip services,
creates a deployment and verifies `ready`. Forgejo mirrors source outward to GitHub after duplicate
deployment authority is removed.

## Task 1: Freeze source and operating baseline

- [x] Record sanitized repository, refs, LFS/submodule, workflow and Docker build facts.
- [x] Discover the current production image/revision, architecture, process, ports, domain, health,
      environment/secret names, SQLite and persistent mounts without exposing values.
- [x] Discover protected Forgejo, GitHub and OpenShip credential mechanisms and validate only the
      required API capabilities.

## Task 2: Create reversible backup

- [x] Capture exact current deployment configuration and image identity in a restricted timestamped
      backup directory on the operating host.
- [x] Produce an SQLite online backup and verify integrity while preserving WAL consistency.
- [x] Inventory and back up required persistent state; document the existing recording backup path.
- [x] Rehearse or mechanically validate the exact image/config/data rollback commands.

## Task 3: Prepare and verify release code

- [x] Change recorder shutdown to fan out every worker stop before waiting and make the process-wait
      path the sole FFmpeg signal owner; add focused concurrency/idempotency regressions.
- [x] Review and minimally amend the production Dockerfile for the Forgejo source label, target
      architecture, non-root runtime, pinned dependencies and health tooling.
- [x] Implement `.forgejo/workflows/build-publish-deploy.yml` with the required trigger, concurrency,
      temporary credentials, shared lock, buildx publish and bounded OpenShip deployment state machine.
- [ ] Add `docs/deployment.md` with resolved non-secret production facts and rollback procedures.
- [x] Run web lint/build, Go tests/build, Docker target-platform build and workflow static checks.

## Task 4: Register Forgejo and mirror

- [x] Verify or create the Forgejo repository, preserve complete refs and set the correct default branch.
- [x] Keep Forgejo as local `origin`; add the existing GitHub repository as `github` without embedded
      credentials.
- [x] Remove duplicate GitHub production deployment authority before mirror activation.
- [x] Register the one-way Push Mirror and verify branch/tag parity and empty `last_error`.

## Task 5: Register OpenShip and repository settings

- [x] Discover the external OpenShip API base ending in `/api/proxy/api`, target agent and host platform.
- [x] Create or reuse the production project with Git integration, webhook and auto deploy disabled.
- [x] Create the image-only CamStation service with verified command, ports, health, restart, environment,
      secrets, persistent volumes, network and pull credential. Because OpenShip 0.6.9 overwrites
      duplicate host-IP bindings for one container port, represent the two verified private LOC
      interfaces with three IPv4 wildcard publish entries only while no public/overlay interface exists.
- [x] Register and verify Forgejo Variables and Secrets by name/presence only.

## Task 6: Publish and deploy

- [ ] Quiesce the legacy recorder without duplicate FFmpeg signals, verify every just-closed segment,
      and perform the controlled first handoff only after the full media backup is protected.
- [ ] Push a reviewed preparation commit without the workflow, bootstrap its exact SHA image through
      the protected manual Registry credential, and verify the controlled first OpenShip deployment.
- [ ] Commit the workflow and final state documentation only after the bootstrap health gates pass,
      then push that second commit to the Forgejo default branch.
- [ ] Verify runner pickup, build lock, buildx platform output, Registry manifest and the second exact
      image tag; require that Actions-driven OpenShip deployment to reach `ready`.
- [ ] Verify service PATCH, deployment creation and `ready` within the fifteen-minute gate.

## Task 7: Prove service behavior and close the transition

- [ ] Verify exact running image, restart count, recent logs, internal health, external TLS request,
      both existing physical LOC endpoints, database/mounts, recorder and scheduled jobs.
- [ ] Perform one controlled follow-up immutable-SHA redeploy and prove persistent state remains intact.
- [ ] Confirm GitHub receives the Forgejo-originated removal/disable commit and starts no production job.
- [ ] Complete deployment documentation, todo Review and lessons with secret-free evidence.
