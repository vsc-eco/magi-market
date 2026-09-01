# magi-market build/test automation.
#
# The wasm artifacts under test/artifacts/ are gitignored, so a fresh clone has
# none of them and `go test ./test/` fails on the go:embed directives in
# test/helpers_test.go. Everything the harness needs is buildable from source:
#
#   * main.wasm                          -> ./contract (this repo)
#   * dexmock/utxomock/feetoken/
#     mintnftmock .wasm                  -> test/mocks/<name>/contract (this repo)
#   * token.wasm / nft.wasm              -> the magi_token / magi_nft repos
#
# The mocks are NOT the production dex/utxo contracts — they are minimal
# stand-ins living in this repo (test/mocks/dexmock/contract/main.go etc.).
#
# Quick start on a fresh clone — this one command does everything (~2 min):
#   make test       # clone deps -> write go.work -> build all 7 wasm -> run suite
#
# Then, to produce the deployable contract binary:
#   make build      # -> bin/main.wasm
#
# Nothing needs to be pre-cloned. token.wasm/nft.wasm are always built from
# vsc-eco `main` fetched from the hardcoded URLs below; a local checkout, if
# present, is reused only as a fetch cache and is never mutated. A missing
# repo (including go-vsc-node) is cloned into .build/src/.
#
# Optional overrides (absolute paths / any git URL or path):
#   VSC_NODE=...   go-vsc-node checkout (the contract test harness)
#   NFT_REPO=...   magi_nft-contract checkout
#   TOKEN_REPO=... magi_token-contract checkout
#   NFT_URL=... NFT_REF=... TOKEN_URL=... TOKEN_REF=...  build a different ref

SHELL := /bin/bash
ROOT  := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

# TinyGo 0.39 refuses to run under a go1.26 host toolchain, and go-vsc-node's
# go.mod floor is go1.25.7 — so pin both build and test to the same toolchain.
GOTOOLCHAIN ?= go1.25.7
export GOTOOLCHAIN

TINYGO ?= tinygo
GO     ?= go

TINYGO_FLAGS := -gc=custom -scheduler=none -panic=trap -no-debug -target=wasm-unknown

# Optional local checkouts, reused as a fetch cache when present. Nothing here
# is required: anything missing is cloned from the canonical vsc-eco URL into
# .build/src/. Override if yours live elsewhere:
#   make artifacts NFT_REPO=$HOME/src/magi_nft-contract
VSC_NODE   ?= $(realpath $(ROOT)/../testnet/go-vsc-node)
NFT_REPO   ?= $(realpath $(ROOT)/../magi_nft-contract)
TOKEN_REPO ?= $(realpath $(ROOT)/../testnet/magi_token-contract)

VSC_NODE_URL ?= https://github.com/vsc-eco/go-vsc-node.git

ART    := $(ROOT)/test/artifacts
MOCKS  := dexmock utxomock feetoken mintnftmock callermock hostilenft
MOCK_WASM := $(addprefix $(ART)/,$(addsuffix .wasm,$(MOCKS)))

.PHONY: all setup build artifacts ext test test-run clean clean-artifacts distclean preflight tools help
.DEFAULT_GOAL := help

all: artifacts ## Build every wasm artifact the test harness embeds

## ---------------------------------------------------------------- setup ----

setup: go.work ## Create go.work (clones go-vsc-node if no local checkout)

# go.mod carries an absolute `replace vsc-node => /home/tibfox/...` that only
# resolves on the original author's machine. go.work (gitignored) overrides it
# with the path on THIS machine, so generate it rather than editing go.mod.
#
# go-vsc-node supplies the wasm contract-test harness. If no local checkout is
# found it is cloned from VSC_NODE_URL. Note this pins the harness to whatever
# `main` currently is: the harness API has changed before (test/helpers_test.go
# once targeted an older 3-return ct.Call), so if a freshly-cloned harness
# fails to compile, point VSC_NODE at a known-good checkout instead.
go.work:
	@set -e; \
	node="$(VSC_NODE)"; \
	if [ -z "$$node" ] || [ ! -f "$$node/go.mod" ]; then \
	  node="$(BUILDDIR)/src/go-vsc-node"; \
	  if [ ! -f "$$node/go.mod" ]; then \
	    echo "no local go-vsc-node checkout — cloning $(VSC_NODE_URL)"; \
	    mkdir -p "$$(dirname "$$node")"; \
	    git clone --quiet --depth 1 "$(VSC_NODE_URL)" "$$node" || { \
	      echo "ERROR: clone failed. Pass an existing checkout instead:"; \
	      echo "  make setup VSC_NODE=/abs/path/to/go-vsc-node"; exit 1; }; \
	  fi; \
	fi; \
	printf 'go 1.24.0\n\nuse .\n\nreplace vsc-node => %s\n' "$$node" > $@; \
	echo "wrote go.work -> vsc-node = $$node"

# Fail fast with actionable guidance rather than a bare "command not found"
# from deep inside a recipe, and only after minutes of cloning.
TINYGO_WANT := 0.39

preflight: ## Check that git / go / tinygo are present and usable
	@ok=1; \
	for t in git go $(TINYGO); do \
	  command -v $$t >/dev/null 2>&1 || { \
	    echo "MISSING: $$t"; ok=0; }; \
	done; \
	if [ $$ok -eq 0 ]; then \
	  echo ""; \
	  echo "This build needs: git, go (>= 1.25.7), tinygo $(TINYGO_WANT).x"; \
	  echo "  tinygo: https://tinygo.org/getting-started/install/"; \
	  echo "  go:     https://go.dev/dl/  (GOTOOLCHAIN=$(GOTOOLCHAIN) is fetched automatically)"; \
	  exit 1; \
	fi; \
	have=$$($(TINYGO) version 2>/dev/null | awk '{print $$3}'); \
	case "$$have" in \
	  $(TINYGO_WANT).*) ;; \
	  *) echo "WARNING: tinygo $$have detected, this repo is verified on $(TINYGO_WANT).x."; \
	     echo "         A different version may produce a wasm the harness rejects.";; \
	esac

tools: ## Print the toolchain versions this Makefile will use
	@$(TINYGO) version
	@$(GO) version
	@echo "GOTOOLCHAIN=$(GOTOOLCHAIN)"
	@echo "VSC_NODE=$(VSC_NODE)"
	@echo "NFT_REPO=$(NFT_REPO)"
	@echo "TOKEN_REPO=$(TOKEN_REPO)"

## ---------------------------------------------------------------- build ----

build: bin/main.wasm ## Build the marketplace contract to bin/main.wasm

bin/main.wasm: $(wildcard contract/*.go) $(wildcard sdk/*.go) $(wildcard runtime/*.go)
	@mkdir -p $(ROOT)/bin
	$(TINYGO) build $(TINYGO_FLAGS) -o $@ ./contract

# The harness embeds test/artifacts/main.wasm, NOT bin/main.wasm — a stale copy
# here silently gives false-positive passes, so tests always depend on this.
$(ART)/main.wasm: bin/main.wasm
	@mkdir -p $(ART)
	cp $< $@

# Each mock is its own module with a vendored sdk/runtime, so build with
# GOWORK=off to keep this repo's workspace out of it.
$(ART)/%.wasm: test/mocks/%/contract/main.go test/mocks/%/sdk/sdk.go
	@mkdir -p $(ART)
	cd $(ROOT)/test/mocks/$* && GOWORK=off $(TINYGO) build $(TINYGO_FLAGS) -o $@ ./contract

artifacts: preflight $(ART)/main.wasm $(MOCK_WASM) ext ## Build contract + mocks + external token/nft wasm

## ------------------------------------------------- external contracts ------

# token.wasm and nft.wasm are built from the canonical vsc-eco `main`, fetched
# straight from the URLs below — NOT from whatever branch a local checkout
# happens to be sitting on, and NOT assuming a local checkout exists at all.
#
# Why the URL is the source of truth: these repos habitually sit on feature
# branches locally, forks are common (magi_nft-contract's `origin` is the
# tibfox fork, vsc-eco is `upstream`), and the market's raw-state decoders
# hardcode these contracts' INTERNAL storage layouts (see README's
# "External-contract coupling risk"). What we build against is a fund-safety
# input, so it gets pinned explicitly rather than inferred.
#
# If a local checkout exists it is reused as the object store (fast, offline
# -friendly) but only ever as a *fetch source cache* — the ref built is always
# what the remote says. If none exists, it is cloned into .build/src/.
# Either way the build happens in a throwaway `git archive` tree, so a local
# checkout is never checked out, stashed, or otherwise mutated.
#
# To build something else, point the URL anywhere git understands (including a
# local path) and name the ref:
#   make -B ext NFT_URL=/path/to/magi_nft-contract NFT_REF=my-feature-branch
BUILDDIR := $(ROOT)/.build

NFT_URL   ?= https://github.com/vsc-eco/magi_nft-contract.git
TOKEN_URL ?= https://github.com/vsc-eco/magi_token-contract.git
NFT_REF   ?= main
TOKEN_REF ?= main

# $(1)=label $(2)=local checkout to reuse (may not exist) $(3)=url $(4)=ref $(5)=out
define build_ext
	set -e; \
	url="$(3)"; ref="$(4)"; \
	if [ -d "$(2)/.git" ]; then src="$(2)"; depth=""; \
	else \
	  src="$(BUILDDIR)/src/$(1)"; depth="--depth 1"; \
	  if [ ! -d "$$src/.git" ]; then \
	    echo "no local $(1) checkout — cloning $$url"; \
	    mkdir -p "$$(dirname "$$src")"; \
	    git clone --quiet --depth 1 "$$url" "$$src" || { \
	      echo "ERROR: clone of $$url failed (offline? private repo?)"; exit 1; }; \
	  fi; \
	fi; \
	git -C "$$src" fetch --quiet $$depth "$$url" "$$ref" || { \
	  echo "ERROR: could not fetch '$$ref' from $$url"; exit 1; }; \
	sha=$$(git -C "$$src" rev-parse --short FETCH_HEAD); \
	dest="$(BUILDDIR)/tree/$(1)"; \
	rm -rf "$$dest"; mkdir -p "$$dest" $(ART); \
	git -C "$$src" archive FETCH_HEAD | tar -x -C "$$dest"; \
	cd "$$dest" && GOWORK=off $(TINYGO) build $(TINYGO_FLAGS) -o "$(5)" ./contract; \
	printf 'built %s @ %s from %s (%s)\n' "$(1).wasm" "$$sha" "$$url" "$$ref"
endef

ext: $(ART)/token.wasm $(ART)/nft.wasm ## Build token.wasm + nft.wasm from vsc-eco main

$(ART)/nft.wasm:
	@$(call build_ext,nft,$(NFT_REPO),$(NFT_URL),$(NFT_REF),$@)

$(ART)/token.wasm:
	@$(call build_ext,token,$(TOKEN_REPO),$(TOKEN_URL),$(TOKEN_REF),$@)

## ----------------------------------------------------------------- test ----

test: preflight go.work artifacts test-run ## Build fresh artifacts, then run the full suite (~80s)

# Bare test run against whatever artifacts are on disk. Use `make test` unless
# you know the artifacts are current.
test-run:
	cd $(ROOT) && $(GO) test ./test/ -count=1

## ---------------------------------------------------------------- clean ----

# Deliberately spares .build/src: that holds auto-cloned repos, and go.work may
# point `vsc-node` at .build/src/go-vsc-node — deleting it would break the build
# with a confusing "missing go.mod". Use `distclean` to drop those too.
clean: ## Remove build output and the badger test DB
	rm -rf $(ROOT)/bin $(BUILDDIR)/tree $(ROOT)/test/data/badger

clean-artifacts: clean ## Also drop every embedded wasm artifact (forces a full rebuild)
	rm -f $(ART)/*.wasm

distclean: clean-artifacts ## Also drop auto-cloned repos in .build/src (re-run `make setup` after)
	rm -rf $(BUILDDIR)

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
