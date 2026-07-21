---
name: project-docs-review
description: Review whether this payment-service repository documentation matches current code and report stale, missing, incorrect, or undocumented completed behavior without editing files.
--- 

Confirm the review scope. Compare only relevant `docs/`, README, configuration examples, migrations, routes, and implementation paths. Do not scan all documentation or code by default; explain the reason before expanding scope.

Identify stale, missing, incorrect, and completed-but-undocumented behavior across API, database, architecture, security, deployment, environment variables, and business flow. Treat implementation as evidence; do not infer completion from plans, comments, or unmerged intent.

Produce a review report only. List recommended additions, modifications, and deletions with document path, heading or proposed placement, code evidence, and rationale. Make no file changes. After user confirmation, hand the approved items to `project-docs-update` for minimal updates.
