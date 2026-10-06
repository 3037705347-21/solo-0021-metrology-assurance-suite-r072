package repository

import (
	"context"
	"testing"
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
)

type seededCase struct {
	device domain.Device
	item   domain.VerificationCase
	seal   *domain.EvidenceSeal
	values []domain.Measurement
}

// seedPurgeFixture stores two devices and four cases:
//   - oldCase: sealed before the cutoff, with a seal, measurements, and events
//   - newCase: sealed after the cutoff, fully retained
//   - openCase: awaiting_measurement (never sealed), fully retained
//   - recheckCase: recheck_required (never sealed), fully retained
func seedPurgeFixture(t *testing.T, repo *Repository, cutoff time.Time) map[string]seededCase {
	t.Helper()
	ctx := context.Background()
	old := cutoff.Add(-48 * time.Hour)
	future := cutoff.Add(48 * time.Hour)

	created := make(map[string]seededCase)
	err := repo.Mutate(ctx, func(m *Mutation) error {
		store := func(tag string, state domain.CaseState, sealedAt *time.Time) (domain.Device, domain.VerificationCase) {
			device, _ := domain.NewDevice(m.NextID("device"), tag, "Bench Meter", "Room A", "Lin", old)
			if err := m.PutDevice(device); err != nil {
				t.Fatalf("put device: %v", err)
			}
			deviceEvent, _ := domain.NewAuditEvent(
				m.NextID("event"), "device", device.ID, "", "device_enrolled",
				"Lin", "device enrolled for assurance work", old,
			)
			m.AppendEvent(deviceEvent)

			item, _ := domain.NewVerificationCase(m.NextID("case"), device.ID, 25.0, 0.5, "Qiao", old)
			switch state {
			case domain.CaseSealed:
				if err := item.RecordMeasurement(true, old); err != nil {
					t.Fatalf("accept measurement: %v", err)
				}
				if err := item.Seal(*sealedAt); err != nil {
					t.Fatalf("seal case: %v", err)
				}
			case domain.CaseRecheckRequired:
				if err := item.RecordMeasurement(false, old); err != nil {
					t.Fatalf("reject measurement: %v", err)
				}
			}
			if sealedAt == nil {
				item.SealedAt = nil
			}
			if err := m.PutCase(item); err != nil {
				t.Fatalf("put case: %v", err)
			}
			return device, item
		}

		oldStamp := old
		oldDevice, oldItem := store("MET-2001", domain.CaseSealed, &oldStamp)
		oldMeasurement1, _ := domain.NewMeasurement(m.NextID("measurement"), oldItem.ID, 25.2, 25.0, 0.5, "Rui", old)
		oldMeasurement2, _ := domain.NewMeasurement(m.NextID("measurement"), oldItem.ID, 25.1, 25.0, 0.5, "Rui", old)
		m.AppendMeasurement(oldMeasurement1)
		m.AppendMeasurement(oldMeasurement2)
		oldSeal, _ := domain.NewEvidenceSeal(
			m.NextID("seal"), oldItem.ID, oldDevice.ID, oldMeasurement1.ID,
			"Verifier", "sealed for archive", old,
		)
		m.PutSeal(oldSeal)
		oldOpened, _ := domain.NewAuditEvent(m.NextID("event"), "case", oldItem.ID, oldItem.ID, "case_opened", "Qiao", "opened", old)
		oldSealedEvent, _ := domain.NewAuditEvent(m.NextID("event"), "case", oldItem.ID, oldItem.ID, "case_sealed", "Verifier", "sealed", old)
		m.AppendEvent(oldOpened)
		m.AppendEvent(oldSealedEvent)
		created["oldCase"] = seededCase{device: oldDevice, item: oldItem, seal: &oldSeal, values: []domain.Measurement{oldMeasurement1, oldMeasurement2}}

		newStamp := future
		newDevice, newItem := store("MET-2002", domain.CaseSealed, &newStamp)
		newMeasurement, _ := domain.NewMeasurement(m.NextID("measurement"), newItem.ID, 25.1, 25.0, 0.5, "Rui", future)
		m.AppendMeasurement(newMeasurement)
		newSeal, _ := domain.NewEvidenceSeal(
			m.NextID("seal"), newItem.ID, newDevice.ID, newMeasurement.ID,
			"Verifier", "sealed recently", future,
		)
		m.PutSeal(newSeal)
		newEvent, _ := domain.NewAuditEvent(m.NextID("event"), "case", newItem.ID, newItem.ID, "case_sealed", "Verifier", "sealed", future)
		m.AppendEvent(newEvent)
		created["newCase"] = seededCase{device: newDevice, item: newItem, seal: &newSeal, values: []domain.Measurement{newMeasurement}}

		_, openItem := store("MET-2003", domain.CaseAwaitingMeasurement, nil)
		openEvent, _ := domain.NewAuditEvent(m.NextID("event"), "case", openItem.ID, openItem.ID, "case_opened", "Qiao", "opened", old)
		m.AppendEvent(openEvent)
		created["openCase"] = seededCase{item: openItem}

		recheckDevice, recheckItem := store("MET-2004", domain.CaseRecheckRequired, nil)
		recheckMeasurement, _ := domain.NewMeasurement(m.NextID("measurement"), recheckItem.ID, 26.5, 25.0, 0.5, "Rui", old)
		m.AppendMeasurement(recheckMeasurement)
		recheckEvent, _ := domain.NewAuditEvent(m.NextID("event"), "case", recheckItem.ID, recheckItem.ID, "measurement_rejected", "Rui", "out of tolerance", old)
		m.AppendEvent(recheckEvent)
		created["recheckCase"] = seededCase{device: recheckDevice, item: recheckItem, values: []domain.Measurement{recheckMeasurement}}
		return nil
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	return created
}

func TestPurgeReleasesOnlyExpiredSealedCasesAtomically(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	repo := New()
	fixture := seedPurgeFixture(t, repo, cutoff)

	stats, err := repo.Purge(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	oldCase := fixture["oldCase"]
	if stats.Cases != 1 || stats.Measurements != 2 || stats.Seals != 1 || stats.Events != 2 {
		t.Fatalf("purge stats = %+v, want 1 case / 2 measurements / 1 seal / 2 events", stats)
	}
	if len(stats.CaseIDs) != 1 || stats.CaseIDs[0] != oldCase.item.ID {
		t.Fatalf("purged ids = %v, want [%s]", stats.CaseIDs, oldCase.item.ID)
	}

	err = repo.Read(ctx, func(snapshot Snapshot) error {
		// The released case and every one of its facts must be unreadable.
		if _, ok := snapshot.Case(oldCase.item.ID); ok {
			t.Error("released case is still readable")
		}
		if _, _, _, ok := snapshot.Evidence(oldCase.item.ID); ok {
			t.Error("evidence seal of released case is still readable")
		}
		if values := snapshot.Measurements(oldCase.item.ID); len(values) != 0 {
			t.Errorf("measurements of released case survive: %d", len(values))
		}
		if values := snapshot.Events(oldCase.item.ID); len(values) != 0 {
			t.Errorf("audit events of released case survive: %d", len(values))
		}
		// Device records, including device-level audit events, are untouched.
		if _, ok := snapshot.Device(oldCase.device.ID); !ok {
			t.Error("device of released case was removed")
		}

		// Sealed but still within retention: fully intact.
		newCase := fixture["newCase"]
		if _, ok := snapshot.Case(newCase.item.ID); !ok {
			t.Error("recently sealed case was purged")
		}
		seal, measurements, events, ok := snapshot.Evidence(newCase.item.ID)
		if !ok {
			t.Error("evidence of recently sealed case is missing")
		} else {
			if seal.ID != newCase.seal.ID {
				t.Errorf("seal = %s, want %s", seal.ID, newCase.seal.ID)
			}
			if len(measurements) != 1 || len(events) != 1 {
				t.Errorf("new case facts = %d measurements, %d events", len(measurements), len(events))
			}
		}

		// Unsealed work in progress, however old, is untouched.
		openCase := fixture["openCase"]
		if read, ok := snapshot.Case(openCase.item.ID); !ok || read.State != domain.CaseAwaitingMeasurement {
			t.Errorf("awaiting case altered: ok=%v", ok)
		}
		recheckCase := fixture["recheckCase"]
		read, ok := snapshot.Case(recheckCase.item.ID)
		if !ok || read.State != domain.CaseRecheckRequired {
			t.Fatalf("recheck case altered: ok=%v state=%s", ok, read.State)
		}
		if values := snapshot.Measurements(recheckCase.item.ID); len(values) != 1 {
			t.Errorf("recheck case measurements = %d, want 1", len(values))
		}
		if values := snapshot.Events(recheckCase.item.ID); len(values) != 1 {
			t.Errorf("recheck case events = %d, want 1", len(values))
		}
		if _, ok := snapshot.Device(recheckCase.device.ID); !ok {
			t.Error("device of recheck case was removed")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	// A sweep with a cutoff before any seal time releases nothing and changes nothing.
	empty, err := repo.Purge(ctx, cutoff.Add(-72*time.Hour))
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if empty.Cases != 0 {
		t.Fatalf("early cutoff purged %d cases", empty.Cases)
	}

	// After the later cutoff passes, the still-sealed retained case is released and
	// the unsealed cases continue to survive.
	second, err := repo.Purge(ctx, cutoff.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("final purge: %v", err)
	}
	if second.Cases != 1 || second.Measurements != 1 || second.Seals != 1 || second.Events != 1 {
		t.Fatalf("final purge stats = %+v", second)
	}
	err = repo.Read(ctx, func(snapshot Snapshot) error {
		if _, ok := snapshot.Case(fixture["openCase"].item.ID); !ok {
			t.Error("awaiting case purged by final sweep")
		}
		if _, ok := snapshot.Case(fixture["recheckCase"].item.ID); !ok {
			t.Error("recheck case purged by final sweep")
		}
		if _, _, _, ok := snapshot.Evidence(fixture["newCase"].item.ID); ok {
			t.Error("newly released case left orphan evidence")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify final: %v", err)
	}
}

// Cases that survived the sweep must keep working: an in-progress case can
// still be reopened after the sweep ran, and its measurements stay reachable.
func TestUnsealedCaseStillWorkableAfterSweep(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	repo := New()
	fixture := seedPurgeFixture(t, repo, cutoff)
	recheck := fixture["recheckCase"].item

	stats, err := repo.Purge(ctx, cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if stats.Cases != 1 {
		t.Fatalf("stats.Cases = %d, want 1", stats.Cases)
	}

	err = repo.Mutate(ctx, func(m *Mutation) error {
		item, ok := m.Case(recheck.ID)
		if !ok {
			t.Fatal("recheck case disappeared")
		}
		if err := item.Reopen(cutoff.Add(-time.Hour)); err != nil {
			return err
		}
		return m.UpdateCase(item)
	})
	if err != nil {
		t.Fatalf("reopen after sweep: %v", err)
	}
}
