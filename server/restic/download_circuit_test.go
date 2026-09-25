package restic

import (
	"errors"
	"testing"
	"time"
)

func TestDownloadCircuitBlocksAndRecovers(t *testing.T) {
	now := time.Date(2026, 8, 16, 7, 16, 0, 0, time.FixedZone("CST", 8*60*60))
	circuit := newDownloadCircuit(30 * time.Minute)
	circuit.now = func() time.Time { return now }

	access := circuit.begin("synology")
	if !access.allowed || access.probe {
		t.Fatalf("initial access = %+v, want regular allowed access", access)
	}
	blocked := circuit.complete("synology", access, errors.New(
		`unexpected error: <title>405</title> errors.aliyun.com 访问被阻断`,
	))
	if !blocked.blocked || blocked.retryAfter != 30*time.Minute {
		t.Fatalf("blocked event = %+v", blocked)
	}

	access = circuit.begin("synology")
	if access.allowed || access.retryAfter != 30*time.Minute {
		t.Fatalf("blocked access = %+v", access)
	}

	now = now.Add(30 * time.Minute)
	probe := circuit.begin("synology")
	if !probe.allowed || !probe.probe {
		t.Fatalf("probe access = %+v", probe)
	}
	concurrent := circuit.begin("synology")
	if concurrent.allowed {
		t.Fatalf("concurrent probe should remain blocked: %+v", concurrent)
	}

	recovered := circuit.complete("synology", probe, nil)
	if !recovered.recovered {
		t.Fatalf("recovery event = %+v", recovered)
	}
	if access = circuit.begin("synology"); !access.allowed || access.probe {
		t.Fatalf("access after recovery = %+v", access)
	}
}

func TestDownloadCircuitIgnoresOtherErrors(t *testing.T) {
	circuit := newDownloadCircuit(30 * time.Minute)
	access := circuit.begin("synology")
	event := circuit.complete("synology", access, errors.New("connection reset"))
	if event.blocked || event.recovered {
		t.Fatalf("unexpected circuit event: %+v", event)
	}
	if next := circuit.begin("synology"); !next.allowed {
		t.Fatalf("ordinary error blocked repository: %+v", next)
	}
}
