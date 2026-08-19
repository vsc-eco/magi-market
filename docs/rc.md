# Bucket RC costs

What bucket sales actually cost, per scenario, with the inventory that produced
each number.

Every figure comes from a test run against the real `magi_nft` and `magi_token`
contracts at the **default `rcLimit` of 10000** the SDK sends — so "fits" below
means a real user can afford it, not merely that the contract is correct.

Regenerate after any change that could move them:

```sh
make test                                   # rebuild wasm first, or you measure stale code
go test ./test/ -run TestScenario -v | grep RCCOST
```

---

## Pokémon booster box

**Inventory:** 2 designs, 30 cards — `common` ×24 in pool 0, `rare` ×6 in pool 1
**Sold as:** packs of 5, `packDraws [4,1]` — 4 commons + 1 guaranteed rare
**Supports:** 6 packs (limited by the 6 rares, one per pack)

| who | operation | RC |
|---|---|---:|
| seller | mint 2 designs (24 + 6 copies) | 910 |
| seller | `setApprovalForAll` | 166 |
| seller | `listBucket` — 2 entries, 2 pools | 2,271 |
| seller | **total to launch** | **3,347** |
| buyer | open one pack (5 cards) | **2,937 – 3,033** |

Setup amortises to ~558 RC per pack across the six it supports.

---

## Raffle — one grand prize

**Inventory:** 2 designs, 41 cards — `grandprize` ×1 and `consolation` ×40, both pool 0
**Sold as:** ten-ticket strips, `packDraws [10]`
**Supports:** 4 strips

| who | operation | RC |
|---|---|---:|
| seller | mint 2 designs (1 + 40 copies) | 883 |
| seller | `setApprovalForAll` | 166 |
| seller | `listBucket` — 2 entries, 1 pool | 2,123 |
| seller | **total to launch** | **3,172** |
| buyer | draw a 10-ticket strip | **3,978 – 4,221** |

One pool, so the jackpot is not slot-guaranteed — it is simply 1-in-41 per
ticket and cannot be won twice.

---

## Playing-card deck — 52 unique 1-of-1s

**Inventory:** 52 designs, 52 cards — every card a 1-of-1, all in pool 0
**Sold as:** 13-card hands, `packDraws [13]`
**Supports:** 4 hands (the whole deck)

| who | operation | RC |
|---|---|---:|
| seller | `mintBatch` ×3 (24 + 24 + 4 ids) | 12,274 |
| seller | `setApprovalForAll` | 166 |
| seller | `listBucket` — first 24 entries | 4,831 |
| seller | `addToBucket` — next 24 | 3,975 |
| seller | `addToBucket` — final 4 | 1,012 |
| seller | **total to launch** | **22,258** |
| buyer | deal a 13-card hand | **7,580 – 8,436** |

**This is the expensive shape.** 52 distinct designs cost 12,274 RC to mint and
9,818 to stock across three transactions — and launching it needs more than the
10k free tier, so the seller must hold HBD in the VSC ledger.

The 13-card hand at ~8,400 is the **most expensive purchase measured**, using
84% of a transaction's budget. Larger hands over this many entries would need
splitting.

---

## Four-tier mystery pack

**Inventory:** 4 designs, 38 cards — `common` ×20 (pool 0), `uncommon` ×12
(pool 1), `holo` ×4 (pool 2), `secret` ×2 (pool 3)
**Sold as:** packs of 10, `packDraws [5,3,1,1]` — one slot per tier
**Supports:** 2 packs (limited by the 2 secrets)

| who | operation | RC |
|---|---|---:|
| seller | mint 4 designs (20 + 12 + 4 + 2 copies) | 1,748 |
| seller | `setApprovalForAll` | 166 |
| seller | `listBucket` — 4 entries, 4 pools | 3,152 |
| seller | **total to launch** | **5,066** |
| buyer | open one pack (10 cards) | **4,453 – 4,485** |

Four pools cost ~880 RC more to list than two, and ~1,470 more per pack than a
5-card two-pool pack. Tiered guarantees are affordable.

---

## Large buckets (measured separately)

| inventory | operation | RC |
|---|---|---:|
| 500 designs × 1 card, 1 pool | stocking, 21 calls | ~128,000 |
| | one single draw | 2,738 |
| | one 10-card pack | 9,001 |
| 2 designs / 500 cards (450 + 50), 2 pools | `listBucket`, 1 call | 2,152 |
| | one 10-card pack | 3,916 |

**Units are free; distinct designs cost.** The same 500 cards cost 2,152 RC to
list as two editions and ~128,000 as 500 unique 1-of-1s — roughly 60× — and the
packs cost buyers 2.3× more. Unique-per-card earns that only if each card must
be individually tradeable and traceable.

---

## Reference costs

| operation | RC |
|---|---:|
| `mint` — one design, any number of copies | ~420 – 465 |
| `mintBatch` — 24 distinct ids | 4,694 (~196 per id) |
| `setApprovalForAll` — once per collection | 166 |
| payment-token `approve` — once per buyer | ~195 |

One-time infrastructure, excluded from the launch totals above because it is
paid once for a whole marketplace rather than per product: `paytoken` init 781,
`nft` init 1,298, `market` init 1,032, `addPaymentToken` ~170 each.

## Planning rule of thumb

```
RC ≈ 1840 + 13 × draws × (entries/32 + min(entries, 32) + 8)
```

The fixed ~1,840 is the payment machinery — pulling funds, paying fee, royalty
and seller. **Every sale path in the contract pays it**, not just buckets, which
is why packs are far better value per card than repeated single draws: one fixed
cost amortised across the whole pack.

Treat it as a **conservative bound, not a predictor** — it runs 15–25% either
side of measured values. `MaxDrawWork = 600` uses it in exactly that spirit: to
refuse an oversized purchase up front with a clear message rather than let it
die deep in execution with "cost limit exceeded".

## Caveats

- Measured in the **in-process harness**. Devnet figures differ slightly (a
  10-card pack over 500 entries measured 9,113 there vs 9,001 here) because the
  fixture differs, not the contract.
- These assume the seller authorised with `setApprovalForAll`. Per-token
  allowances add a state read per entry.
- A seller who is **not** the collection owner pays one extra read per entry for
  the soulbound check.
- RC is a **per-account** budget: an account's allowance is its VSC-ledger HBD
  balance plus a 10k free tier. Launch totals above are what the seller spends
  in aggregate, so anything past ~10k needs HBD on the ledger.
