// Package repro reproduces the paper "attack -> defense" chains that Bounty
// 1.0 integrates into its runtime:
//
//  1. BIPIA (KDD '25)          - <data> prompt boundary on untrusted web content
//  2. RAGworm / DonkeyRail (CCS '25) - self-replication prompt detection
//  3. Mind the Web (ASIA CCS '26)    - task-aligned injection detection
//  4. CoT Leakage (ASIA CCS '26)     - fanout-layer secret redaction
//
// Run with: go test ./repro/ -v
// See docs/repro-papers-2026-08-05.md for the full reproduction report.
package repro
