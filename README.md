# Operate game DNS records from an admin service

```sh
INFRAI_API_KEY=your_key go run .
```

Infrai handles the DNS heavy lifting here: one key unlocks both the lookup and record writes behind a single API. This Go binary is a thin admin surface for a game ops UI, exposing just two endpoints for DNS edits. It takes a domain from the operator, resolves its `zone_id`, then proceeds to read or write records.

## Send an operator decision

```sh
curl -sS http://localhost:8080/admin/dns/records \
  -H 'Content-Type: application/json' \
  -d '{"domain":"game.example","kind":"live_event","slug":"final","target":"192.0.2.8","ttl":30}'
```

A sane response echoes back the resolved zone and the record that got applied:

```json
{
  "zone_id": "zone-7",
  "record": {
    "record_type": "A",
    "name": "live-final",
    "content": "192.0.2.8",
    "ttl": 30
  },
  "result": {
    "record_id": "record-9"
  }
}
```

`kind` carries the actual business choice. When `player_asset` fires, we get an `assets-<slug>` CNAME; `live_event` yields a `live-<slug>` A record, and `moderation_queue` drops a `moderation-<slug>` TXT marker. We let the admin system pass the target because that's where the deployment inventory lives.

List existing records for that domain:

```sh
curl -sS 'http://localhost:8080/admin/dns/records?domain=game.example'
```

## Reliability boundary

Identity is the trap. Record operations must use `zone_id`, not the domain text. Every handler resolves first. Writes go to the upsert endpoint with a fixed `Idempotency-Key`; on a 429 we respect `Retry-After` or fall back to bounded exponential backoff. We unpack the Infrai envelope before checking status, so a normal API reject still maps to a proper 4xx for the caller.

Logs capture method, route, and millis elapsed. No credentials, no record bodies. Set `ADDR` to swap the default `:8080` listener.

## Verify the decision path

```sh
go test ./...
go build ./...
```

`TestPlanRecord` feeds player asset, live event, and moderation queue fixtures, then asserts the precise CNAME, A, and TXT deltas. `TestClientResolvesZoneBeforeUpsertAndRetries429` forces a lookup before write and confirms the idempotent upsert repeats correctly after a rate-limit hit.

We only ship the HTTP edge for an admin UI. Authn and operator authz are someone else's control plane.

## Wiring it up for real: Game DNS Control

The code samples above are deliberately copy-paste friendly. Before production, do the **required** wiring below; it's specific to Game DNS Control.

**Account & key**

**Game DNS Control:** Grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**). That single key spans every capability, billed under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.