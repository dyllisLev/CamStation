# Live Layout Initialization and Deletion Design

**Date:** 2026-07-12
**Status:** Approved

## Goal

Make `/live` restore the selected saved layout reliably on first navigation and let operators permanently delete saved layouts.

## Current Problem

`LiveWorkspace` initializes as soon as cameras are available. If the camera query finishes before the layout query, the still-loading layout result is treated as an empty saved-layout list and `defaultLayout` is installed. The `layout.length > 0` guard then prevents the saved layout from being applied when it arrives. A refresh can change request timing and hide the problem.

Layout persistence currently supports list, create, and update only. There is no delete operation in the store, HTTP API, frontend API, query hooks, or live workspace.

## Design

### Deterministic initialization

The live workspace will not initialize its tile layout until both camera and saved-layout queries have completed successfully. It will then initialize once:

1. Read the last selected layout ID from local storage.
2. Use that saved layout when it still exists; otherwise use the most recently updated saved layout.
3. If there are no saved layouts, create the in-memory default layout.

The workspace will keep its existing layout during later background query refreshes so an operator's unsaved edits are not overwritten.

If either initial query fails, the workspace will show an error state instead of treating the failed response as an empty list. A normal query retry can then initialize the workspace after both results succeed.

### Persistent deletion

Add `DeleteLayout(ctx, id)` to the SQLite store and expose it as `DELETE /api/layouts/{id}`. A missing ID returns `404`; a successful deletion returns `204 No Content`.

Add the corresponding frontend API method and TanStack Query mutation. Successful deletion invalidates the `layouts` query.

### Operator interaction

Each saved-layout row in the right panel will have a delete action. Selecting it opens a Korean confirmation prompt containing the layout name.

After a successful deletion:

- Deleting a non-current layout leaves the current view and last-selected ID unchanged.
- Deleting the current layout loads the first remaining layout from the existing newest-first ordering and stores its ID as the last selected layout.
- Deleting the final layout installs the in-memory default layout, clears the current ID and last-selected ID, and marks the default as unsaved.

The delete control will be a separate button rather than a button nested inside the existing row button, preserving valid and accessible markup. While its request is pending, repeat deletion is disabled. A failed request leaves the current layout untouched and reports the failure to the operator.

## Data and Safety

Deletion removes only the selected row from `layouts`. It does not affect cameras, recordings, stream workers, or generated configuration. The endpoint accepts only the opaque path ID and does not expose runtime paths or credentials.

## Verification

- A frontend unit test exercises isolated initialization state logic with the camera-first/layout-second order and proves the saved layout wins only after both queries settle.
- Store tests cover successful deletion and missing IDs.
- Route characterization covers `DELETE /api/layouts/{id}`, including the response code and absence from a later list.
- Frontend tests cover the post-delete selection rule as isolated state logic.
- Run `cd web && npm test`, `cd web && npm run lint`, `cd web && npm run build`, `go test ./...`, and `go build -o camstationd ./cmd/camstationd`.

## Out of Scope

- Soft deletion or recovery history
- Bulk layout deletion
- Layout renaming
- Synchronizing the last selected layout across browsers
