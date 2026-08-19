# Bucket events — the indexer contract

Everything an indexer needs to mirror bucket state comes from these events. The
indexer is mapping-driven: an attribute that is not emitted cannot be stored, so
these payloads are an interface, and one that is expensive to change after
deployment — a fix needs a contract update *and* a reindex.

`TestBucketEventsMirrorState` pins the shape below.

## `bucket_listed`

Carries the full commercial terms and the entries, so a bucket can be rendered
without reading contract state.

```json
{"type":"bucket_listed","attributes":{
  "bucketId":0,"seller":"hive:tibfox","nftContract":"nft",
  "paymentToken":"paytoken","pricePerDraw":"0","pricePerPack":"5000",
  "packDraws":[4,1],"expirationBlock":0,
  "feeBps":250,"royaltyBps":0,"royaltyRecipient":"",
  "entries":[{"tokenId":"boostercommon","amount":24,"pool":0},
             {"tokenId":"boosterrare","amount":6,"pool":1}],
  "entryCount":2,"units":30},"tx":"..."}
```

`entries` is safe to inline because `listBucket` accepts at most
`MaxEntriesPerCall` (24) of them — one event can never carry an unbounded
array. A large bucket arrives as a listing plus a series of restocks, each
bounded the same way.

`feeBps` / `royaltyBps` / `royaltyRecipient` are the values **snapshotted at
list time**, which is what the bucket will actually pay out — not whatever the
collection is configured with later.

## `bucket_restocked`

```json
{"bucketId":0,"seller":"hive:tibfox",
 "entries":[{"tokenId":"evextra","amount":2,"pool":0}],
 "added":1,"totalEntries":3,"unitsAdded":2}
```

Restocking is append-only: a token id already in the bucket cannot be added
again, so entries here are always new.

## `bucket_draw` — one per delivered unit

```json
{"bucketId":0,"buyer":"hive:ash","tokenId":"boostercommon","pool":0,"drawIndex":0}
```

`pool` matters: a mirror tracking units per pool needs to know which one to
decrement, and pool balance is not derivable from the totals.

## `bucket_purchase` — one per transaction

```json
{"bucketId":0,"buyer":"hive:ash","mode":"pack","draws":5,
 "paymentToken":"paytoken","paid":"5000","fee":"125","royalty":"0","unitsLeft":25}
```

`unitsLeft` is a **reconciliation point**, deliberately. A mirror that only
accumulates deltas desyncs permanently the moment it misses one event; a mirror
that reads `unitsLeft` re-anchors on every purchase.

## `bucket_entry_dropped`

```json
{"bucketId":0,"tokenId":"evextra","pool":0,"units":2,"reason":"seller no longer holds it"}
```

Emitted when the seller no longer holds an entry or has revoked approval. The
market never escrows, so stock can go stale, and `units` says how many
disappeared — otherwise a mirror cannot decrement correctly.

## `bucket_sold_out`

```json
{"bucketId":0,"seller":"hive:tibfox"}
```

Emitted after the `bucket_purchase` that empties the bucket, so a consumer sees
the sale that closed it before it sees the close. Without this event the only
observable close is a seller delisting, and a bucket that simply sold out would
read as open forever.

## `bucket_delisted`

```json
{"bucketId":0,"seller":"hive:tibfox"}
```

The seller closing it deliberately. Distinct from selling out, and both leave
unsold stock with the seller.

## Rebuilding state from the log

| bucket state | from |
|---|---|
| terms, pack shape, expiry | `bucket_listed` |
| fee / royalty as it will actually pay | `bucket_listed` snapshot fields |
| contents and per-pool composition | `bucket_listed` + `bucket_restocked` entries |
| units remaining | `bucket_purchase.unitsLeft` (authoritative), adjusted by `bucket_entry_dropped.units` |
| who bought what | `bucket_draw` per unit, `bucket_purchase` per transaction |
| open / closed | `bucket_sold_out` or `bucket_delisted`; expiry is derivable from `expirationBlock` |

**A bucket with guaranteed slots may never sell out.** The guaranteed pool
empties first and strands whatever is left in the others, so `bucket_sold_out`
is not certain to arrive — track per-pool units rather than assuming a bucket
drains evenly.
