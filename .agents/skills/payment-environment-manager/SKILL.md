---
name: payment-environment-manager
description: Plan, review, validate, or prepare production and sandbox environment separation for the nnviopp payment-service, including Compose, environment files, Nginx, NewebPay, callbacks, migrations, acceptance, rollback, and production change gates.
---

Read the root `AGENTS.md` and `payment-service/docs/環境隔離設計.md` first. Treat production changes as external-impact actions: list impact, commands, verification, and rollback, then stop for user approval.

Inspect only the relevant Compose, environment example, Nginx draft, config validation, callback flow, migration runner, and tests. Never print secrets or read production `.env` values unless explicitly required and safely redacted.

Verify that production and sandbox have distinct Compose project names, API/DB container names, network, volumes, DB name/user, HMAC/session/merchant secrets, callback destinations, receipt storage, and logs. Verify production targets `core.newebpay.com` with mock and test callbacks disabled; verify sandbox targets `ccore.newebpay.com` or an approved internal mock and cannot reach production callbacks.

Run focused tests, then `go test ./...`, `go build ./cmd/api`, and `docker compose --env-file <environment-file> config` when safe. Report the environment mapping, failures, acceptance evidence, rollback plan, and unresolved manual work. Do not deploy, alter DNS/TLS, restart Nginx, migrate production, or copy production data without explicit approval.
