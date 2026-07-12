# Production Deployment Guide

This guide documents the target production deployment for the Lightsail Instance migration.

## Topology

```text
Internet
  -> AWS Lightsail Static IP
  -> Nginx on host (80/443)
  -> 127.0.0.1:8080
  -> payment-api container
  -> mysql container
  -> Docker volume (mysql-data)
```

## Files Added For Production

- `compose.yaml`: runs `payment-api` and `mysql`
- `deploy/nginx/payment.conf`: host Nginx reverse proxy template
- `.env.example`: full production environment variable checklist
- `.env.prod.example`: production env template for dual-stack deployment
- `.env.test.example`: test env template for dual-stack deployment
- `.env`: local server-only template, ignored by Git

If you want to run production and test on the same VPS, also read `docs/dual-environment-namecheap-vps.md`.

## Confirmed Callback And Return Routes

The following routes are defined in `internal/delivery/http/router.go` and should be used for production integration:

- NewebPay `NotifyURL`: `/api/v1/deposits/providers/newebpay/notifications`
- NewebPay `ReturnURL`: `/api/v1/deposits/payment-result`
- gateway payout callback: `/api/payments/callback`

Compatibility aliases still exist, but production should use the canonical routes above:

- Legacy NewebPay notify alias: `/notify/newebpay`
- Legacy payment result alias: `/payment/result`

## Required Environment Variables

Fill the following values in `.env` on the server:

- `DATABASE_DSN`
- `MYSQL_ROOT_PASSWORD`
- `MYSQL_DATABASE`
- `MYSQL_USER`
- `MYSQL_PASSWORD`
- `NEWEBPAY_MPG_URL`
- `NEWEBPAY_MERCHANT_ID`
- `NEWEBPAY_HASH_KEY`
- `NEWEBPAY_HASH_IV`
- `NEWEBPAY_NOTIFY_URL`
- `NEWEBPAY_RETURN_URL`
- `GATEWAY_BASE_URL`
- `GATEWAY_CUSTOMER_ID`
- `GATEWAY_SIGN_KEY`
- `GATEWAY_PAYOUT_NOTIFY_URL`
- `GATEWAY_MAX_SKEW_SECONDS`
- `GATEWAY_HTTP_TIMEOUT_SECONDS`
- `MERCHANT_CODE`
- `MERCHANT_NAME`
- `MERCHANT_API_KEY`
- `MERCHANT_CALLBACK_URL`
- `MERCHANT_INITIAL_BALANCE_TWD`
- `PAYOUT_REVIEW_TOKEN`

## Server Deployment Steps

Run on the Lightsail instance:

```bash
cd /opt/payment/payment-service
cp .env.example .env
vi .env
docker compose build
docker compose up -d
docker compose ps
docker compose logs -f payment-api
```

Notes:

- `DATABASE_DSN` must point to `mysql:3306`, not `127.0.0.1:3306`
- The app runs database migration automatically on startup via `migrations/001_init.sql`
- MySQL data persists in the Docker volume `mysql-data`
- The API is published only on `127.0.0.1:8080` and is intended to be reached through host Nginx

## Install And Configure Nginx

Run on the host:

```bash
sudo apt update
sudo apt install -y nginx
sudo mkdir -p /var/www/certbot
sudo cp /opt/payment/payment-service/deploy/nginx/payment.conf /etc/nginx/sites-available/payment.conf
sudo ln -sf /etc/nginx/sites-available/payment.conf /etc/nginx/sites-enabled/payment.conf
sudo nginx -t
sudo systemctl reload nginx
```

Before DNS is ready, you can temporarily set `server_name` to the instance public hostname or `_` for testing.

## DNS

Point the production hostname to the Lightsail static IP:

- `api.nnviopp.com -> 18.162.105.240`

Do not request Let's Encrypt certificates until the DNS record has propagated publicly.

## HTTPS With Certbot

After the domain resolves correctly:

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d api.nnviopp.com
sudo systemctl status certbot.timer
```

After Certbot succeeds:

1. Update `deploy/nginx/payment.conf` with the real domain if needed.
2. Enable the `443` server block in `/etc/nginx/sites-available/payment.conf`.
3. Run `sudo nginx -t && sudo systemctl reload nginx`.

## Production Integration Values

Use the confirmed production integration values below:

- `NEWEBPAY_NOTIFY_URL=https://api.nnviopp.com/api/v1/deposits/providers/newebpay/notifications`
- `NEWEBPAY_RETURN_URL=https://api.nnviopp.com/api/v1/deposits/payment-result`
- `GATEWAY_BASE_URL=https://api.nnviopp.com`
- `GATEWAY_CUSTOMER_ID=RIG001`
- `GATEWAY_SIGN_KEY=23ebf933a239994547bcafbca6c7fb0ed8a2b041dded453885d8c40788347725`
- `GATEWAY_PAYOUT_NOTIFY_URL=https://api.nnviopp.com/api/payments/callback`
- `GATEWAY_MAX_SKEW_SECONDS=300`
- `GATEWAY_HTTP_TIMEOUT_SECONDS=15`
- `MERCHANT_CODE=RIG001`
- `MERCHANT_NAME=NewebPay`

`MERCHANT_CALLBACK_URL` is not an inbound route in this service. It is the merchant system URL that this service will call back to.

## Verification Checklist

```bash
curl -i http://127.0.0.1:8080/health
curl -I http://api.nnviopp.com/health
curl -I https://api.nnviopp.com/health
docker compose ps
docker compose logs --tail=100 payment-api
docker compose logs --tail=100 mysql
```

Expected results:

- `/health` returns `200 OK`
- `payment-api` starts without migration errors
- `mysql` stays healthy
- Nginx proxies requests to the app successfully
- HTTPS certificate is valid after Certbot

## Operations Recommendations

- Keep `.env` only on the server and never commit it.
- Back up the Docker volume or database regularly.
- Restrict SSH source IPs if possible.
- Consider adding a host firewall rule to allow `80/443/22` only.
- Rotate `MERCHANT_API_KEY`, `PAYOUT_REVIEW_TOKEN`, and third-party secrets on a schedule.
- Add external uptime monitoring for `/health`.
- Add log shipping if transaction auditability is important.

## Current Manual Dependencies

These values are still pending and block final go-live:

- Final merchant callback URL and merchant API key
- Final NewebPay production callback secrets and merchant-side webhook URL
