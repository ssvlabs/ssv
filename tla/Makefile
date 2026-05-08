# TLA+ formal-verification runner for OBFT SAFETY specs.
#
# Usage:
#   make verify-bare      run BareOBFT_Safety (fast, completes in seconds)
#   make verify-lbid      run LBid_Safety (no timeout; overnight-friendly)
#   make verify-lbidnew   run LBidNew_Safety (no timeout; overnight-friendly)
#   make verify-all       run all three sequentially
#   make clean            remove TLC checkpoints and run logs
#   make help             this message
#
# Each verify-* target produces (under runs/):
#   <spec>-<timestamp>.log       full TLC stdout/stderr
#   <spec>-<timestamp>.summary   parsed digest -- share this back for docs
#
# Variables (override on the command line, e.g. `make verify-lbid HEAP=16g`):
#   HEAP        Java heap, default: 24g
#   WORKERS     TLC workers, default: auto
#   JAVA        java binary, default: /opt/homebrew/opt/openjdk/bin/java
#   TLA_TOOLS   tla2tools.jar location, default: tla2tools.jar
#
# Ctrl-C during a run still produces a partial-coverage summary file.
# See README.md for the full verification methodology and prior results.

HEAP      ?= 24g
WORKERS   ?= auto
JAVA      ?= /opt/homebrew/opt/openjdk/bin/java
TLA_TOOLS ?= tla2tools.jar
RUNS_DIR  := runs

export HEAP WORKERS JAVA TLA_TOOLS RUNS_DIR

.PHONY: help verify-bare verify-lbid verify-lbidnew verify-all clean

help:
	@echo "TLA+ formal-verification targets:"
	@echo "  make verify-bare      BareOBFT_Safety  (fast, ~seconds)"
	@echo "  make verify-lbid      LBid_Safety      (no timeout, overnight-friendly)"
	@echo "  make verify-lbidnew   LBidNew_Safety   (no timeout, overnight-friendly)"
	@echo "  make verify-all       all three sequentially"
	@echo "  make clean            remove TLC checkpoints and run logs"
	@echo
	@echo "Each run produces:"
	@echo "  $(RUNS_DIR)/<spec>-<timestamp>.log       full TLC stdout/stderr"
	@echo "  $(RUNS_DIR)/<spec>-<timestamp>.summary   parsed digest (share this back)"
	@echo
	@echo "Variables (override on the command line):"
	@echo "  HEAP=$(HEAP)         Java heap"
	@echo "  WORKERS=$(WORKERS)   TLC worker count"
	@echo "  JAVA=$(JAVA)"
	@echo "  TLA_TOOLS=$(TLA_TOOLS)"

verify-bare:
	@./scripts/tlc-run.sh BareOBFT_Safety

verify-lbid:
	@./scripts/tlc-run.sh LBid_Safety

verify-lbidnew:
	@./scripts/tlc-run.sh LBidNew_Safety

verify-bareliveness:
	@./scripts/tlc-run.sh BareOBFT_Liveness

verify-all: verify-bare verify-lbid verify-lbidnew

clean:
	rm -rf states/* $(RUNS_DIR)
