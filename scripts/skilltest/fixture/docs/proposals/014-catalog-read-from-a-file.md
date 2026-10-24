---
id: 014
title: The catalog is read from catalog.json, so a price changes without a rebuild
kind: feature
size: M
judge: opus
status: proposed
lens: eng
proposed_by: t3-5b2c9e41
proposed_on: 2026-10-23
---

## Opportunity
The catalog is a Go map in `cart`. Every price change is a code change, a review and a new binary, and the shop has had three price changes this month.

## Proposal
`cart.LoadCatalog(path)` reads `catalog.json` (`SHOP_CATALOG` names another), a JSON object of item names and prices in cents; `cmd/shop` loads it before checkout, and the map in `cart` stays as the catalog when the file is missing. In: `cart`, `cmd/shop`, the README. Out: editing prices from the command line.

Judge: opus, because a missing file, a bad price and an item in old orders that left the catalog each need a decision.

## What you'll see
Editing `catalog.json` changes the next checkout's prices, with no rebuild.

## Verification
`go test ./...`, with tests for a missing file, a malformed one and a price of 0.

## Risks and the alternative considered
A typo in the file stops checkout: the error names the file and the line. Considered: flags for each price, which no one would keep in step.

## Needs from the operator
