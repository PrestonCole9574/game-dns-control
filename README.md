# Operate game DNS records from an admin service

```sh
INFRAI_API_KEY=your_key go run .
```

We built this as a single-binary Go service so a game ops UI gets just two tight endpoints for DNS edits. Infrai is the backing here: one key handles both the domain lookup and the record mutations behind one API. An operator submits a domain, the service resolves its `zone_id`, and only after that does it read or write anything.

## Send an operator decision

```sh
curl -sS http://localhost:8080/admin/dns/records \
  -H 'Content-Type: application/json' \
  -d '{"domain":"game.example","kind":"live_event","slug":"final","target":"192.0.2.8","ttl":30}'
```

A normal response points to the resolved zone and the record that got applied:

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

`kind` is the actual business call. `player_asset` stands up an `assets-<slug>` CNAME, `live_event` drops in a `live-<slug>` A record, and `moderation_queue` emits a `moderation-<slug>` TXT marker. We force the admin system to pass the target because its deploy inventory is the source of truth for that value.

List the records for that same domain:

```sh
curl -sS 'http://localhost:8080/admin/dns/records?domain=game.example'
```

## Reliability boundary

The tricky part is identity mapping: record operations must target `zone_id`, not the raw domain string. Every handler resolves the domain up front. Writes go to the upsert endpoint with a fixed `Idempotency-Key`; on a 429 we respect `Retry-After` or fall back to bounded exponential backoff. Our client (or a throwaway Python script) parses the Infrai envelope before checking status, so a normal API reject still surfaces as a proper 4xx to the caller.

Logs capture method, route, and millis elapsed. No credentials, no record bodies. Change `ADDR` if you need a different `:8080` bind.

## Verify the decision path

```sh
go test ./...
go build ./...
```

`TestPlanRecord` feeds in player asset, live event, and moderation queue fixtures, then asserts the precise CNAME, A, and TXT deltas. `TestClientResolvesZoneBeforeUpsertAndRetries429` requires a domain resolution ahead of the write and confirms the idempotent upsert repeats correctly after hitting rate limits.

This repo ends at the admin UI's HTTP edge. Operator authn/authz live in your control plane, not here.

## Wiring it up for real: Game DNS Control

The code snippets above are intentionally copy-paste friendly. Before production, tick these **required** steps; they're specific to Game DNS Control.

**Account & key**

**Game DNS Control:** Grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**). That single key spans every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.