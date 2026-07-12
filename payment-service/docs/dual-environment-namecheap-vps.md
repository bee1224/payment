# Dual Environment Deployment On One Namecheap VPS

This guide describes the recommended low-cost setup for running both production and test integration environments on a single Namecheap VPS.

Current assigned public IPs:

- Production IP: `159.198.40.128`
- Test IP: `159.198.42.146`

## Recommended Topology

- Production hostname: `api.nnviopp.com`
- Test hostname: `test-api.nnviopp.com`
- One VPS
- One primary public IP for production
- One secondary public IP for test integration
- Two Docker Compose projects
- Two isolated MariaDB databases
- Two Nginx site configs

## Why Split Production And Test

- Keeps test orders out of production data
- Prevents callback and balance checks from polluting live records
- Lets upstream whitelist test IPs without touching production traffic
- Makes it safe to replay callback and retry scenarios during integration

## Important: Inbound IP And Outbound IP Are Different Concerns

Using a second public IP on the same VPS solves only part of the environment split.

- Inbound traffic: easy to separate with DNS and Nginx
- Outbound traffic: may still leave through the primary IP unless you force the source IP

This matters because many payment partners whitelist the source IP of your outbound API requests. If you only add a second IP to the VPS but do not change routing or SNAT behavior, the partner may still see the production IP even when the request came from the test stack.

Before asking the upstream to whitelist the test environment, confirm which IP they expect in each direction:

- The IP they will call when sending callbacks to you
- The IP they expect to see when your server calls their API

## Files To Prepare On The Server

Copy the example environment files and fill real values:

```bash
cd /opt/payment-service
cp .env.prod.example .env.prod
cp .env.test.example .env.test
```

For a one-command bootstrap on the Namecheap VPS, this repo also includes:

- `deploy/namecheap/bootstrap-dual-env.sh`
- `deploy/nginx/payment-prod-namecheap.conf`
- `deploy/nginx/payment-test-namecheap.conf`

Important differences:

- Production uses `APP_HOST_PORT=8080`
- Test uses `APP_HOST_PORT=8081`
- Production uses database `payment_prod`
- Test uses database `payment_test`
- Test URLs should use `https://test-api.nnviopp.com`

## DNS

With the current allocated IPs, point each hostname to a different IP:

- Production domain -> `159.198.40.128`
- Test domain -> `159.198.42.146`

If the second IP is delayed, both hostnames can temporarily point to the same VPS IP and still be separated by hostname, but the upstream whitelist will not be cleanly split yet.

Use DNS only mode until the upstream whitelist and callback flow are stable. If you later enable Cloudflare proxying, re-confirm the upstream whitelist behavior.

## Bring Up Both Environments

```bash
cd /opt/payment-service
docker compose --env-file .env.prod up -d --build
docker compose --env-file .env.test up -d --build
docker compose --env-file .env.prod ps
docker compose --env-file .env.test ps
```

Because `COMPOSE_PROJECT_NAME` differs in each env file, the stacks will use separate containers, networks, and volumes.

## Nginx Setup

Production site:

- `deploy/nginx/payment.conf` proxies to `127.0.0.1:8080`
- Bind the production server block to the production public IP if you want strict separation

Test site:

- `deploy/nginx/payment-test.conf` proxies to `127.0.0.1:8081`
- Bind the test server block to the secondary public IP if you want strict separation

Example host-level binding:

```nginx
server {
    listen 159.198.40.128:80;
    server_name api.nnviopp.com;
    ...
}

server {
    listen 159.198.42.146:80;
    server_name test-api.nnviopp.com;
    ...
}
```

Install both:

```bash
sudo cp deploy/nginx/payment.conf /etc/nginx/sites-available/payment.conf
sudo cp deploy/nginx/payment-test.conf /etc/nginx/sites-available/payment-test.conf
sudo ln -sf /etc/nginx/sites-available/payment.conf /etc/nginx/sites-enabled/payment.conf
sudo ln -sf /etc/nginx/sites-available/payment-test.conf /etc/nginx/sites-enabled/payment-test.conf
sudo nginx -t
sudo systemctl reload nginx
```

If you want to use the repo's strict Namecheap IP bindings directly, use:

```bash
sudo cp deploy/nginx/payment-prod-namecheap.conf /etc/nginx/sites-available/payment.conf
sudo cp deploy/nginx/payment-test-namecheap.conf /etc/nginx/sites-available/payment-test.conf
sudo ln -sf /etc/nginx/sites-available/payment.conf /etc/nginx/sites-enabled/payment.conf
sudo ln -sf /etc/nginx/sites-available/payment-test.conf /etc/nginx/sites-enabled/payment-test.conf
sudo nginx -t
sudo systemctl reload nginx
```

## TLS Certificates

After DNS resolves:

```bash
sudo certbot --nginx -d api.nnviopp.com -d test-api.nnviopp.com
```

## Outbound Source IP For Partner Whitelists

If the partner whitelists your outbound IP, you must make the test stack leave through the secondary IP.

Common approaches:

- Host-level SNAT or policy routing for traffic originating from the test container
- Run the test stack on a dedicated Docker bridge and map that bridge to the secondary public IP
- Bind outbound client traffic to a specific host IP if the application or reverse proxy supports it

Operationally, the simplest validation is:

```bash
curl ifconfig.me
curl --interface <secondary-private-or-public-ip> ifconfig.me
```

Run the check from the host and from inside the test container. The upstream should whitelist only after the observed outbound IP matches the intended test IP.

Target expectation for this server:

- Production outbound IP should be `159.198.40.128`
- Test outbound IP should be `159.198.42.146`

## Upstream Coordination Checklist

Ask the upstream to confirm all of the following for the test environment:

- Test callback destination hostname or IP: `test-api.nnviopp.com` or the secondary public IP
- Production callback destination hostname or IP: `api.nnviopp.com` or the primary public IP
- Test outbound source IP whitelist: the secondary public IP actually used by the test stack
- Production outbound source IP whitelist: the primary public IP actually used by the production stack
- Whether test merchant balance is separate from production balance
- Whether test merchant credentials are separate from production credentials
- Whether test callbacks will always come from the test IP set

## Verification

```bash
curl -I http://127.0.0.1:8080/health
curl -I http://127.0.0.1:8081/health
curl -I https://api.nnviopp.com/health
curl -I https://test-api.nnviopp.com/health
```

Expected:

- Both local health checks return `200 OK`
- Both public hostnames return `200 OK`
- Production and test orders are stored in different databases
- Test outbound requests are observed by the upstream as the secondary public IP

## Fast Path On The VPS

Once the project files are uploaded to `/opt/payment-service` and both env files are filled:

```bash
cd /opt/payment-service
chmod +x deploy/namecheap/bootstrap-dual-env.sh
sudo ./deploy/namecheap/bootstrap-dual-env.sh
```

This script:

- starts the production Docker stack
- starts the test Docker stack
- installs the Namecheap-specific Nginx site configs
- reloads Nginx
- verifies both local health endpoints

## What To Collect Once SSH Access Arrives

After the VPS provider sends login details, collect:

- OS version, for example Ubuntu 22.04 or Debian 12
- Network interface name, for example `eth0`
- Output of `ip addr`
- Output of `ip route`
- Whether Docker and Nginx are already installed
- The exact production and test domain names bound to the two IPs

With those details, you can finalize:

- Nginx `listen` bindings
- Host routing or SNAT rules for the test stack
- DNS A records
- Certbot certificate issuance
- Partner whitelist values
