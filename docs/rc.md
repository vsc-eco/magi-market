# Bucket RC costs

Measured numbers for what bucket sales actually cost, by scenario.

Every figure here comes from a test run against the real `magi_nft` and
`magi_token` contracts, at the **default `rcLimit` of 10000** that the SDK
sends — so "fits" below means a real user can afford it, not merely that the
contract is correct.

Regenerate after any change that could move them:

```sh
make test                                   # rebuild wasm first, or you measure stale code
go test ./test/ -run TestScenario -v | grep RCCOST
```

The scenario tests emit their own costs, so this file is derived from a run
rather than hand-maintained.

## What a buyer pays

| scenario | purchase | cards | RC | per card |
|---|---|---:|---:|---:|
| Pokémon booster | pack `[4,1]` — 4 commons + 1 guaranteed rare | 5 | **~3,000** | 600 |
| Raffle | strip `[10]` — 1 jackpot among 40 blanks | 10 | **~4,000** | 400 |
| Four-tier mystery | pack `[5,3,1,1]` across 4 pools | 10 | **~4,470** | 447 |
| Playing-card deck | hand `[13]` from 52 unique entries | 13 | **~8,000** | 615 |
| Big bucket, single draw | 1 draw from 500 unique entries | 1 | **2,738** | 2,738 |
| Big bucket, pack | pack `[10]` from 500 unique entries | 10 | **9,001** | 900 |
| Editions, pack | pack `[10]` from 2 entries / 500 units | 10 | **3,916** | 392 |

Spread across repeat runs: Pokémon 2,987–3,033; raffle 3,978–4,221; four-tier
4,453–4,485; deck 7,580–8,436. The variation is the draw landing on different
entries, not noise.

**Everything fits inside one transaction.** The tightest is a 13-card hand over
52 entries at ~8,400, using 84% of the budget. Above roughly that size, split
the purchase.

## What a seller pays

| scenario | operation | RC |
|---|---|---:|
| Pokémon booster | list bucket (2 entries, 2 pools) | 2,271 |
| Raffle | list bucket (2 entries, 41 units) | 2,123 |
| Four-tier mystery | list bucket (4 entries, 4 pools) | 3,152 |
| Playing-card deck | `listBucket` (first 24 entries) | 4,831 |
| | `addToBucket` (next 24) | 3,975 |
| | `addToBucket` (final 4) | 1,012 |
| | **stocking total, 52 entries** | **9,818** |
| Big bucket | stocking 500 unique entries, 21 calls | ~128,000 |

Minting is separate and is the NFT contract's bill, not the market's:

| minting | RC |
|---|---:|
| `mintBatch`, 24 distinct ids | 4,694 (~196 per id) |
| `mint`, one design × 100 copies | 388 |
| `setApprovalForAll` (once per collection) | 166 |

## The number that decides your design

**Units are free; distinct designs cost.** A bucket of 500 cards as *two
editions* lists for 2,152 RC in one call. The same 500 cards as *500 unique
1-of-1s* costs ~128,000 RC across 21 calls, and its packs cost buyers roughly
2.3× more.

Unique-per-card earns that only if each card must be individually tradeable and
traceable. If the cards are interchangeable, use editions.

## Planning rule of thumb

```
RC ≈ 1840 + 13 × draws × (entries/32 + min(entries, 32) + 8)
```

The fixed ~1,840 is the payment machinery — pulling funds, paying fee, royalty
and seller. **Every sale path in the contract pays it**, not just buckets, which
is why packs are so much better value per card than repeated single draws: one
fixed cost amortised across the whole pack.

Treat the formula as a **conservative bound, not a predictor** — it runs 15–25%
either side of measured values. `MaxDrawWork = 600` uses it deliberately in that
spirit: to refuse an oversized purchase up front with a clear message, rather
than let it die deep in execution with "cost limit exceeded".

## Caveats

- Measured in the **in-process harness**. Devnet figures differ slightly (a
  10-card pack over 500 entries measured 9,113 there vs 9,001 here) because the
  fixture differs, not the contract.
- Costs shift with how the seller authorised the market: these assume
  `setApprovalForAll`. Per-token allowances add a state read per entry.
- A seller who is **not** the collection owner pays one extra read per entry for
  the soulbound check.
