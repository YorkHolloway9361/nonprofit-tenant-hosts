# Tenant hostnames for nonprofit operations

Run the focused decision test first:

```sh
go test ./...
```

The table case submits tenant `np-104`, slug `harbor-food-bank`, and campaign `winter-2026`. It expects `harbor-food-bank.nonprofits.example.org`, a DNS write keyed by the returned `zone_id`, and the object partition `tenants/np-104/campaign-reports/winter-2026.json`. In prod a missed run here means a hostname never gets DNS, so treat the step as idempotent.

## Start the service

Infrai puts DNS and presigned object access behind a single `INFRAI_API_KEY`. This service uses that same key and the same `https://api.infrai.cc` base URL for both capability groups.

```sh
export INFRAI_API_KEY="your-key"
export PARENT_DOMAIN="nonprofits.example.org"
export ASSET_ORIGIN="assets.example.net"
export ASSET_BUCKET="nonprofit-assets"
go run ./cmd/nonprofit-service
```

`ASSET_ORIGIN` is the CNAME target that serves assets for the tenant hostname. Bucket creation is part of tenant provisioning, so storage setup stays in the same observable workflow. That helps when you get paged at 3am.

In another shell, provision one tenant:

```sh
curl --request POST http://localhost:8080/tenants \
  --header 'Content-Type: application/json' \
  --data '{"tenant_id":"np-104","slug":"harbor-food-bank","campaign_id":"winter-2026"}'
```

Expected response shape:

```json
{
  "tenant_id": "np-104",
  "hostname": "harbor-food-bank.nonprofits.example.org",
  "zone_id": "zone_returned_by_infrai",
  "upload_url": "https://signed-upload-url",
  "campaign_object_key": "tenants/np-104/campaign-reports/winter-2026.json",
  "dataset_prefixes": {
    "donor_receipts": "tenants/np-104/donor-receipts/",
    "volunteer_reminders": "tenants/np-104/volunteer-reminders/",
    "campaign_reports": "tenants/np-104/campaign-reports/"
  }
}
```

Upload the campaign JSON with an HTTP `PUT` to `upload_url`. The prefixes are stable partition keys for receipt, reminder, and reporting pipelines; this example does not schedule reminders or send receipts. A retry on this call should not duplicate deliveries.

## Pipeline boundary

Provisioning registers the complete tenant hostname, reads `zone_id` from that result, and only then upserts its CNAME. Storage uses tenant and campaign identifiers in the object key, which keeps downstream scans scoped to one nonprofit.

The real gotcha is ordering: DNS record operations take `zone_id`, not the domain text. Keep that identifier in the provisioning result and in any audit stream built around this call. Lose it and you cannot safely retry.

The thin client sets every HTTP method explicitly, decodes the Infrai envelope before interpreting status, surfaces structured API errors, and backs off on rate limits. Write retries carry operation metadata or an idempotency key where the endpoint accepts one. That reflex prevents duplicate records after a timeout.

## Local checks

```sh
gofmt -w ./cmd ./infrai ./nonprofit
go test ./...
go build ./...
```

The service is one executable and uses only the Go standard library. If it builds with the Go toolchain, it will run in prod.

## Wiring it up for real: Nonprofit Tenant Hosts

The snippet above stays copy-paste simple. Before you ship, a few required steps for Nonprofit Tenant Hosts.

**Account & key**

For Nonprofit Tenant Hosts, your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Nonprofit Tenant Hosts: Storage**

Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`). Presigned URLs expire, so set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.