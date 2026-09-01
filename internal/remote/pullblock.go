package remote

import "sync/atomic"

// The global pull-replication block probe (M15 T-422, FR-138.3;
// replication.md §9.2-B-4 — the pull direction of the blockPush/blockPull
// emergency brake).
//
// BinFlow's pull replication IS the remote pull-through plane (ADR-0021
// decision 2), and that engine is assembled INSIDE repo.New — neither cmd
// nor the adapters ever see the concrete *remote.Engine, so an injected
// option or a constructor seam would drag the flag through repo.Service's
// frozen signature. The minimal seam is therefore an INSTALLED probe: the
// cmd assembly (the single writer, at boot) installs a func reporting the
// replication domain's BlockGate state; fetchFlow consults it before any
// upstream contact. An uninstalled probe (tests, degraded assemblies)
// reports false — the pre-T-422 posture, nothing ever blocked.
//
// Semantics while blocked (照既有 remote 语义拒绝/降级, §9.2-B-4 + the
// AC's ruling): ZERO upstream traffic — a copy inside its TTL still serves
// fresh (step 4 never reaches the network), an expired copy degrades stale
// with the block named in the hint, and a miss answers the family's unfound
// 404 (or the hardFail 502) naming the block, exactly the assumed-offline
// downgrade shape. No repository is marked assumed-offline: the brake is an
// operator action, not an upstream fault, and lifting it must restore full
// service with no window to wait out.

// pullBlockProbe is the installed gate reporter (nil = never blocked).
var pullBlockProbe atomic.Pointer[func() bool]

// InstallPullBlockProbe installs the global pull-block reporter. Called
// ONCE at assembly (cmd) with the replication BlockGate's PullBlocked
// method; a second call replaces the first (tests swap probes per case).
func InstallPullBlockProbe(probe func() bool) {
	p := probe
	if p == nil {
		pullBlockProbe.Store(nil)
		return
	}
	pullBlockProbe.Store(&p)
}

// pullBlocked consults the installed probe; false when none is installed.
func pullBlocked() bool {
	if p := pullBlockProbe.Load(); p != nil {
		return (*p)()
	}
	return false
}

// pullBlockSummary is the fetch-outcome line and stale-copy hint while the
// brake is on (names the operator action, never an upstream fault).
const pullBlockSummary = "pull replication is blocked globally (blockPullReplications)"

// PullBlocked reports the currently installed probe's verdict — the test
// seam for asserting an installation landed.
func PullBlocked() bool { return pullBlocked() }
