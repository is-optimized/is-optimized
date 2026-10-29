# Direct origin HTTPS

The GCP Nanos runtime can serve HTTPS directly, without a load balancer or a second process. `HTTPS_ENABLED=true` starts port 80 for ACME HTTP-01 challenges and GET/HEAD redirects, plus port 443 for the application. The plaintext listener does not serve scanning, reports, or contact APIs. Unknown HTTP Host values and TLS SNI names are rejected. Alias hosts redirect to the configured canonical `PUBLIC_URL`; request headers cannot supply the redirect destination.

Runtime metadata keys:

| Key | Deployment value |
| --- | --- |
| `HTTPS_ENABLED` | `true` |
| `PUBLIC_URL` | `https://is-optimized.com` |
| `ACME_HOSTS` | `is-optimized.com,www.is-optimized.com` |
| `ACME_EMAIL` | `david@sire.run` |
| `GCS_TLS_BUCKET` | Dedicated private certificate cache bucket |
| `ACME_DIRECTORY_URL` | Omit for Let's Encrypt production, or use `https://acme-staging-v02.api.letsencrypt.org/directory` for staging |

The public origin hostname is always included in the allowlist. Extra names are explicit, limited to eight total DNS hostnames, and cannot be IPs or wildcards. Listener addresses default to `:80` and `:443`; local test runners may use `HTTP_LISTEN_ADDR` and `HTTPS_LISTEN_ADDR`. Plain local development remains on `LISTEN_ADDR`/8080 with HTTPS disabled. Nanos GCP packaging exposes 80/443 and enables HTTPS; SMS remains disabled.

Certificates, private keys, and the ACME account key are cached in GCS using Application Default Credentials. The bucket must have uniform private access, public access prevention, no lifecycle expiration, and runtime `roles/storage.objectUser` access. It must be separate from the report bucket; the app rejects the same bucket name. Objects use opaque hashed keys under an ACME-directory-specific prefix so staging and production certificates/accounts cannot mix. TLS data never goes through public report handlers, image contents, instance metadata, or logs. Cache operations have bounded size and deadlines; a startup write/read/delete probe fails startup if the cache cannot persist data. An unavailable read is not treated as a cache miss. Later write failures are logged without private contents because autocert may still serve a successfully issued certificate while its cache write fails.

For non-GCP local qualification, explicitly set `ACME_CACHE_DIR` to a durable private directory. The cloud runtime requires GCS, avoiding an assumption that the current Nanos boot filesystem persists certificates. This architecture is single-instance: a shared GCS cache is not a distributed ACME issuance lock, so do not scale replicas without adding certificate coordination.

The listeners are bound before issuance warmup begins. Warmup attempts configured hosts once; autocert handles later requests and renewal. Port binding and `/healthz` process liveness do not prove that a certificate has been issued. Verify a real HTTPS handshake with hostname and certificate-chain validation. The first request may fail while initial issuance is in progress. Go autocert tries TLS-ALPN-01 first and then HTTP-01; Cloudflare does not forward the special ALPN challenge, so the initial failed ALPN authorization can precede successful HTTP-01 fallback. HTTP-01 requires Cloudflare to forward `/.well-known/acme-challenge/*` to origin port 80 without Access challenges, a Worker override, caching, or forced edge HTTPS redirects. Both apex and www DNS must target this origin before their certificates can be issued. Never switch Cloudflare to Flexible as a workaround. Use Full (strict) after a production certificate is verified.

Provider acceptance still requires: Nanos clock and CA roots; ADC access to the dedicated cache; actual ACME issuance and renewal path; valid origin certificate through Cloudflare; and certificate reuse after a same-VM reboot. Changing DNS or enabling these modes is an infrastructure action owned by the deployment coordinator. The local tests use generated fixture certificates and a fake GCS HTTP endpoint; they do not request real certificates or write to cloud storage.

Local verification (2026-10-28): fresh Mac admission passed (load 1.45, pressure 1 throughout, idle 88.86/90.31%, no sustained swap or thermal warning). `GOMAXPROCS=2 go test -p 1 ./cmd/server` passed using Go 1.27.1 and external caches, with generated fixture certificates and a fake GCS HTTP endpoint. The shared lease was released. Logs and admission: `/Volumes/BuildOffload/artifacts/is-optimized-origin-https-20261028/`. Pinned Go 1.26 compilation, full integration checks and actual Nanos/ACME/GCP runtime qualification remain coordinator gates.
